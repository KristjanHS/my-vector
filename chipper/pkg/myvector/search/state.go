package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Monthly hard caps per provider, UTC month (W5).
const (
	CapTavily    = 900
	CapSearxng   = 3000
	CapOpenMeteo = 3000
)

var Caps = map[string]int{"tavily": CapTavily, "searxng": CapSearxng, "open-meteo": CapOpenMeteo}

// Cache TTL per class and size (W6).
const (
	TTLWeather = 30 * time.Minute
	TTLLive    = 30 * time.Minute
	TTLRecent  = 6 * time.Hour
	CacheMax   = 200
)

var ttl = map[Class]time.Duration{Weather: TTLWeather, Live: TTLLive, Recent: TTLRecent}

// StatePath is myvectorSearch.json beside myvectorConfig.json (W7, gitignored).
var StatePath = func() string { return filepath.Join(filepath.Dir(lang.Path()), "myvectorSearch.json") }

type entry struct {
	Block string    `json:"block"`
	At    time.Time `json:"at"`
}

// State is the persisted quota counts, lookup cache and geocodes; every change is written atomically.
type State struct {
	mu     sync.Mutex
	path   string
	Month  string            `json:"month"`
	Used   map[string]int    `json:"used"`
	Cache  map[string]entry  `json:"cache"`
	Places map[string]Place  `json:"places"`
	memo   map[string]memoFC // weather place → its last forecast, RAM only (N11 template + {search} block share one fetch)
}

type memoFC struct {
	p  Place
	f  Forecast
	at time.Time
}

// memoGet is place's forecast fetched within TTLWeather of now.
func (s *State) memoGet(place string, now time.Time) (Place, Forecast, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memo[place]
	if !ok || now.Sub(m.at) >= TTLWeather {
		return Place{}, Forecast{}, false
	}
	return m.p, m.f, true
}

func (s *State) memoPut(place string, p Place, f Forecast, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.memo == nil {
		s.memo = map[string]memoFC{}
	}
	s.memo[place] = memoFC{p, f, now}
}

// Open loads path; a missing or corrupt file starts empty.
func Open(path string) *State {
	s := &State{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, s); err != nil {
			logf("myvector: search state corrupt, starting empty (quota counts reset):", path, err)
			s = &State{}
		}
	}
	s.path = path
	if s.Used == nil {
		s.Used = map[string]int{}
	}
	if s.Cache == nil {
		s.Cache = map[string]entry{}
	}
	if s.Places == nil {
		s.Places = map[string]Place{}
	}
	return s
}

var cur struct {
	sync.Mutex
	s *State
}

// current is the State at StatePath, reopened when the path changes.
func current() *State {
	cur.Lock()
	defer cur.Unlock()
	if p := StatePath(); cur.s == nil || cur.s.path != p {
		cur.s = Open(p)
	}
	return cur.s
}

func month(now time.Time) string { return now.UTC().Format("2006-01") }

// rollover zeroes the counts on a new UTC month; caller holds mu.
func (s *State) rollover(now time.Time) {
	if m := month(now); s.Month != m {
		s.Month, s.Used = m, map[string]int{}
	}
}

// Take spends one call of provider's monthly cap before the call is made (every attempt counts); false =
// capped, skip the provider.
func (s *State) Take(provider string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollover(now)
	if s.Used[provider] >= Caps[provider] {
		return false
	}
	s.Used[provider]++
	s.save()
	return true
}

// UsedThisMonth is provider's count in now's UTC month.
func (s *State) UsedThisMonth(provider string, now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Month != month(now) {
		return 0
	}
	return s.Used[provider]
}

// cacheKey is class + the sentence case-folded with punctuation dropped and spaces collapsed.
func cacheKey(c Class, sentence string) string {
	f := strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return c.String() + ":" + strings.Join(f, " ")
}

// Get is a cached block younger than its class TTL.
func (s *State) Get(c Class, sentence string, now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Cache[cacheKey(c, sentence)]
	if !ok || now.Sub(e.At) >= ttl[c] {
		return "", false
	}
	return e.Block, true
}

// Put caches block, evicting the oldest entries past CacheMax.
func (s *State) Put(c Class, sentence, block string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cache[cacheKey(c, sentence)] = entry{block, now}
	for len(s.Cache) > CacheMax {
		var oldest string
		for k, e := range s.Cache {
			if oldest == "" || e.At.Before(s.Cache[oldest].At) {
				oldest = k
			}
		}
		delete(s.Cache, oldest)
	}
	s.save()
}

func (s *State) place(name string) (Place, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.Places[name]
	return p, ok
}

func (s *State) setPlace(name string, p Place) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Places[name] = p
	s.save()
}

// save writes temp + rename; caller holds mu. A failed write keeps the in-RAM state.
func (s *State) save() {
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	if err := conf.WriteFile(s.path, ".myvectorSearch-*", b, 0600); err != nil {
		logf("myvector: search state write:", err)
	}
}
