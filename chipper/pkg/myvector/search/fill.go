package search

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Timeout bounds the whole Fill on the voice path (plan W10).
var Timeout = 2500 * time.Millisecond

var logf = logger.Println

// weatherPlace is c.WeatherPlace up to any comma (Open-Meteo geocodes a bare name: "Tartu, Estonia" →
// "Tartu"), else zone's city.
func weatherPlace(c Config, zone *time.Location) string {
	p, _, _ := strings.Cut(c.WeatherPlace, ",")
	if p = strings.TrimSpace(p); p != "" {
		return p
	}
	return ZoneCity(zone)
}

// ZoneCity is an IANA zone's city ("Europe/Tallinn" → "Tallinn", "America/New_York" → "New York"); "" for
// zones without one (UTC, Local).
func ZoneCity(zone *time.Location) string {
	if zone == nil {
		return ""
	}
	name := zone.String()
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return strings.ReplaceAll(name[i+1:], "_", " ")
}

var errCapped = errors.New("capped")

func geocode(ctx context.Context, st *State, place string) (Place, error) {
	if p, ok := st.place(place); ok {
		return p, nil
	}
	if !st.Take("open-meteo", time.Now()) {
		return Place{}, errCapped
	}
	p, err := Geocode(ctx, place)
	if err == nil {
		st.setPlace(place, p)
	}
	return p, err
}

// CachedGeocode is Geocode through the saved places and the open-meteo quota (W5, W17).
func CachedGeocode(ctx context.Context, place string) (Place, error) {
	return geocode(ctx, current(), place)
}

// QuotaForecast is FetchForecast after an open-meteo Take (W5, W17).
func QuotaForecast(ctx context.Context, lat, lon float64) (Forecast, error) {
	return forecast(ctx, current(), lat, lon)
}

func forecast(ctx context.Context, st *State, lat, lon float64) (Forecast, error) {
	if !st.Take("open-meteo", time.Now()) {
		return Forecast{}, errCapped
	}
	return FetchForecast(ctx, lat, lon)
}

// Unavailable is the W8 {search} block when no lookup worked.
func Unavailable(l string) string {
	reply := "English"
	if l == lang.Estonian {
		reply = "Estonian"
	}
	return "Live lookup failed: no web or weather data right now. If the question needs live data, say briefly in " +
		reply + " that you can't look it up right now; never guess current facts."
}

// chain is c.Provider (when one is named) then DefaultOrder, deduplicated, keeping providers with a key / URL.
func chain(c Config) []Provider {
	var ps []Provider
	seen := map[string]bool{}
	for _, name := range append([]string{c.Provider}, DefaultOrder...) {
		if p, ok := c.ProviderFor(name); ok && !seen[name] {
			seen[name] = true
			ps = append(ps, p)
		}
	}
	return ps
}

// Fill is the {search} block for sentence after the user's prev asks (newest first): a fresh cached block, else
// Weather from Open-Meteo or Live / Recent over chain (one shared Timeout; a news ask races the chain and merges,
// search-news-quality Q4); "" for class None or Live / Recent with provider off, Unavailable when every lookup failed.
func Fill(sentence string, prev []string, l, brain string, zone *time.Location) string {
	class := Route(sentence, l)
	c := LoadConfig()
	if class == None || (class != Weather && c.Provider == "off") {
		return ""
	}
	st := current()
	start := time.Now()
	q, topic := followQuery(sentence, prev, l)
	// Web blocks are sized per brain (W9). Weather skips the persisted block cache: its RAM forecast memo is the cache,
	// and the lookup keeps it warm for WeatherReply's memo-only read (N12).
	key := brain + " " + topic + " " + sentence
	if topic != "" && class != Weather {
		logf("myvector: search topic", topic)
	}
	if class != Weather {
		if b, ok := st.Get(class, key, start); ok {
			logf("myvector: search", class.String(), "cache", "ok", time.Since(start).Milliseconds())
			return b
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	var block string
	switch {
	case class == Weather:
		block = attempt(ctx, class, "open-meteo", func() (string, error) { return weather(ctx, st, weatherPlace(c, zone), zone) })
	case q.News:
		block = Block(brain, race(ctx, class, st, chain(c), q, l))
	default:
		for _, p := range chain(c) {
			if ctx.Err() != nil {
				logf("myvector: search", class.String(), p.Name(), "timeout", time.Since(start).Milliseconds())
				break
			}
			block = attempt(ctx, class, p.Name(), func() (string, error) {
				rs, err := look(ctx, st, p, q, l)
				if err != nil {
					return "", err
				}
				if b := Block(brain, rs); b != "" {
					return b, nil
				}
				return "", errNoResults
			})
			if block != "" {
				break
			}
		}
	}
	if block == "" {
		return Unavailable(l)
	}
	if class != Weather {
		st.Put(class, key, block, time.Now())
	}
	return block
}

var errNoResults = errors.New("no results")

// look is one provider's search after its quota Take; SearXNG gets English(q) on an et-EE news ask (Q3). Results
// sharing no stem with the query are dropped, and a set left empty is "no results" (search-news-quality N6).
func look(ctx context.Context, st *State, p Provider, q Query, l string) ([]Result, error) {
	if !st.Take(p.Name(), time.Now()) {
		return nil, errCapped
	}
	words := q.Text
	if l == lang.Estonian { // either provider may answer an et ask in English
		words += " " + English(q.Text)
	}
	if p.Name() == "searxng" && l == lang.Estonian && q.News {
		q.Text = English(q.Text)
	}
	rs, err := p.Search(ctx, q, l, SnippetsClaude)
	if err != nil {
		return nil, err
	}
	if kept := Relevant(rs, words); len(kept) < len(rs) {
		logf("myvector: search relevance", p.Name(), "dropped", len(rs)-len(kept), "of", len(rs))
		rs = kept
	}
	if len(rs) == 0 {
		return nil, errNoResults
	}
	return rs, nil
}

// race runs every provider at once until ctx ends and merges their results (Q4).
func race(ctx context.Context, class Class, st *State, ps []Provider, q Query, l string) []Result {
	sets := make([][]Result, len(ps))
	var wg sync.WaitGroup
	for i, p := range ps {
		wg.Add(1)
		go func(i int, p Provider) {
			defer wg.Done()
			attempt(ctx, class, p.Name(), func() (string, error) {
				rs, err := look(ctx, st, p, q, l)
				sets[i] = rs
				return "ok", err
			})
		}(i, p)
	}
	wg.Wait()
	return merge(sets...)
}

// merge interleaves sets in order (first of each, then second, …), dropping repeated URLs (a result without a URL
// is always kept).
func merge(sets ...[]Result) []Result {
	var out []Result
	seen := map[string]bool{}
	for i := 0; ; i++ {
		more := false
		for _, s := range sets {
			if i >= len(s) {
				continue
			}
			more = true
			if u := strings.TrimRight(s[i].URL, "/"); u == "" || !seen[u] {
				seen[u] = true
				out = append(out, s[i])
			}
		}
		if !more {
			return out
		}
	}
}

// attempt runs one provider's lookup and logs its W14 line.
func attempt(ctx context.Context, class Class, name string, look func() (string, error)) string {
	start := time.Now()
	block, err := look()
	line := []any{"myvector: search", class.String(), name, "ok", time.Since(start).Milliseconds()}
	switch {
	case errors.Is(err, errCapped):
		line[3] = "capped"
	case errors.Is(err, context.DeadlineExceeded) || (err != nil && ctx.Err() != nil):
		line[3] = "timeout"
	case err != nil:
		line[3] = "err"
		line = append(line, err.Error())
	}
	logf(line...)
	if err != nil {
		return ""
	}
	return block
}

// weather is place's WeatherBlock from today on (a memo fetched before midnight starts at yesterday, N12), else
// from the forecast's first day when today isn't among its days.
func weather(ctx context.Context, st *State, place string, zone *time.Location) (string, error) {
	p, f, err := weatherData(ctx, st, place)
	if err != nil {
		return "", err
	}
	if g, ok := fromToday(f, placeToday(p, zone, weatherClock())); ok {
		f = g
	}
	return WeatherBlock(p.Name, f), nil
}

// weatherData is place's geocode + forecast, from st's memo when fresh.
func weatherData(ctx context.Context, st *State, place string) (Place, Forecast, error) {
	if place == "" {
		return Place{}, Forecast{}, errors.New("no weather place: set search.weather_place")
	}
	if p, f, ok := st.memoGet(place, time.Now()); ok {
		return p, f, nil
	}
	p, err := geocode(ctx, st, place)
	if err != nil {
		return Place{}, Forecast{}, err
	}
	f, err := forecast(ctx, st, p.Lat, p.Lon)
	if err != nil {
		return Place{}, Forecast{}, err
	}
	st.memoPut(place, p, f, time.Now())
	return p, f, nil
}
