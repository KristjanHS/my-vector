// Package listen is active listening: the on/off phrase matcher and the per-robot on/off state.
package listen

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

// On and Off are whole phrases after quiet.Fillers removal (every saved language).
var (
	On  = []string{"activate active listening", "active listening on", "aktiveeri aktiivne kuulamine", "aktiivne kuulamine sees"}
	Off = []string{"deactivate active listening", "active listening off", "deaktiveeri aktiivne kuulamine", "aktiivne kuulamine välja"}
)

// Model answers every LLM call while on, both languages (P1: listenprobe held-out 16/20, 0 false SPEAK).
const Model = "gemma4:e4b-it-qat"

const (
	IdleOff   = 30 * time.Minute
	NightFrom = 21 // patch 0011 night window, robot local time
	NightTo   = 9
)

// STT variants of "hei vektor" (Stage 1.1 round trip, mari → myvector-stt et-EE).
var (
	mergedWake = regexp.MustCompile(`\b(?:hei|hey)(vector|victor|vektor)\b`)
	eiWake     = regexp.MustCompile(`^\W*ei\W+(vector|victor|vektor)\b`)
)

// Match reports whether text is an on (on=true) or off phrase.
func Match(text string) (on, ok bool) {
	t := strings.ToLower(text)
	t = eiWake.ReplaceAllString(mergedWake.ReplaceAllString(t, "hei $1"), "hei $1")
	got := strings.Join(quiet.Tokens(t), " ")
	for _, p := range On {
		if got == p {
			return true, true
		}
	}
	for _, p := range Off {
		if got == p {
			return false, true
		}
	}
	return false, false
}

type bot struct{ since, lastHeard, lastSpoke time.Time }

// State is RAM-only (a wire-pod restart turns every robot off), keyed by ESN.
type State struct {
	mu   sync.Mutex
	bots map[string]*bot
	Log  func(string)
}

func NewState() *State { return &State{bots: map[string]*bot{}, Log: func(string) {}} }

func (s *State) Activate(esn string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bots[esn] = &bot{since: now}
}

func (s *State) Deactivate(esn, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.off(esn, why)
}

func (s *State) off(esn, why string) {
	if _, ok := s.bots[esn]; ok {
		delete(s.bots, esn)
		s.Log("myvector listen: off (" + why + ")")
	}
}

// Active applies the idle and night auto-off, then reports whether esn is on.
func (s *State) Active(esn string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bots[esn]
	if !ok {
		return false
	}
	last := b.since
	if b.lastHeard.After(last) {
		last = b.lastHeard
	}
	switch {
	case now.Sub(last) >= IdleOff:
		s.off(esn, "idle")
		return false
	case b.since.Before(lastNightStart(now)):
		s.off(esn, "night")
		return false
	}
	return true
}

// Any is Active for every robot: true when one is on (callers without an ESN).
func (s *State) Any(now time.Time) bool {
	s.mu.Lock()
	esns := make([]string, 0, len(s.bots))
	for e := range s.bots {
		esns = append(esns, e)
	}
	s.mu.Unlock()
	on := false
	for _, e := range esns {
		on = s.Active(e, now) || on
	}
	return on
}

// Heard and Spoke stamp an utterance / a reply; a robot that is off is left off.
func (s *State) Heard(esn string, now time.Time) { s.stamp(esn, func(b *bot) { b.lastHeard = now }) }
func (s *State) Spoke(esn string, now time.Time) { s.stamp(esn, func(b *bot) { b.lastSpoke = now }) }

// LastSpoke is the zero time when esn is off or has not replied.
func (s *State) LastSpoke(esn string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bots[esn]; ok {
		return b.lastSpoke
	}
	return time.Time{}
}

func (s *State) stamp(esn string, f func(*bot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bots[esn]; ok {
		f(b)
	}
}

func lastNightStart(now time.Time) time.Time {
	n := time.Date(now.Year(), now.Month(), now.Day(), NightFrom, 0, 0, 0, now.Location())
	if n.After(now) {
		n = n.AddDate(0, 0, -1)
	}
	return n
}
