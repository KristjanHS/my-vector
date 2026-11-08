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
// "aktiveerige"/"deaktiveerige" are et-EE STT variants of the imperative (live log 2026-11-08); the en
// "turn on/off" and "the" forms are the matching everyday phrasings.
var (
	On = []string{"activate active listening", "activate the active listening", "turn on active listening",
		"turn active listening on", "active listening on",
		"aktiveeri aktiivne kuulamine", "aktiveerige aktiivne kuulamine", "aktiivne kuulamine sees"}
	Off = []string{"deactivate active listening", "deactivate the active listening", "turn off active listening",
		"turn active listening off", "active listening off",
		"deaktiveeri aktiivne kuulamine", "deaktiveerige aktiivne kuulamine", "aktiivne kuulamine välja"}
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

type span struct{ from, to time.Time }

type line struct {
	at   time.Time
	text string
}

type bot struct {
	since, lastHeard, lastSpoke time.Time
	echo                        []span // A6 windows, EchoPad included
	lines                       []line // A5 transcript, last Window
	busy                        bool   // an LLM call + reply in flight
	streak                      int    // listen replies in a row (A9 Backoff)
}

// State is RAM-only (a wire-pod restart turns every robot off), keyed by ESN.
type State struct {
	mu    sync.Mutex
	bots  map[string]*bot
	Log   func(string)
	OnOff func(esn string) // every off path, idle + night included; runs under the State lock, so it must not call back into State
}

func NewState() *State {
	return &State{bots: map[string]*bot{}, Log: func(string) {}, OnOff: func(string) {}}
}

func (s *State) Activate(esn string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bots[esn]; ok {
		b.since = now
		return
	}
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
		s.OnOff(esn)
	}
}

// Active applies the idle and night auto-off, then reports whether esn is on.
func (s *State) Active(esn string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active(esn, now)
}

func (s *State) active(esn string, now time.Time) bool {
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
func (s *State) Spoke(esn string, now time.Time) {
	s.stamp(esn, func(b *bot) {
		if b.lastSpoke.IsZero() || now.Sub(b.lastSpoke) >= BackoffReset {
			b.streak = 0
		}
		b.streak++
		b.lastSpoke = now
	})
}

// LastSpoke is the zero time when esn is off or has not replied.
func (s *State) LastSpoke(esn string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bots[esn]; ok {
		return b.lastSpoke
	}
	return time.Time{}
}

// Turn stamps a wake-word ask at: an utterance for IdleOff, and its echo window up to TurnTail.
func (s *State) Turn(esn string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active(esn, at) {
		b := s.bots[esn]
		b.lastHeard = at
		b.addEcho(at, at.Add(TurnTail))
	}
}

// claim takes esn's one in-flight LLM slot: "COOL" inside the Backoff wait unless named, "BUSY" while another utterance holds it.
func (s *State) claim(esn string, now time.Time, named bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bots[esn]
	switch {
	case !ok:
		return "OFF"
	case !named && now.Sub(b.lastSpoke) < b.wait(now):
		return "COOL"
	case b.busy:
		return "BUSY"
	}
	b.busy = true
	return ""
}

func (b *bot) wait(now time.Time) time.Duration {
	if b.streak == 0 || now.Sub(b.lastSpoke) >= BackoffReset {
		return 0
	}
	if b.streak > len(Backoff) {
		return Backoff[len(Backoff)-1]
	}
	return Backoff[b.streak-1]
}

func (s *State) release(esn string) { s.stamp(esn, func(b *bot) { b.busy = false }) }

// Spoken stamps Vector's own speech from..to as an echo window.
func (s *State) Spoken(esn string, from, to time.Time) {
	s.stamp(esn, func(b *bot) { b.addEcho(from, to) })
}

func (b *bot) addEcho(from, to time.Time) {
	keep := b.echo[:0]
	for _, w := range b.echo {
		if w.to.After(from.Add(-Window)) {
			keep = append(keep, w)
		}
	}
	b.echo = append(keep, span{from.Add(-EchoPad), to.Add(EchoPad)})
}

// Echo reports whether start..end overlaps one of esn's echo windows.
func (s *State) Echo(esn string, start, end time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, ok := s.bots[esn]; ok {
		for _, w := range b.echo {
			if start.Before(w.to) && end.After(w.from) {
				return true
			}
		}
	}
	return false
}

// hear returns the transcript lines within Window of now, then appends text.
func (s *State) hear(esn string, now time.Time, text string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bots[esn]
	if !ok {
		return nil
	}
	var before []string
	keep := b.lines[:0]
	for _, l := range b.lines {
		if now.Sub(l.at) <= Window {
			keep = append(keep, l)
			before = append(before, l.text)
		}
	}
	b.lines = append(keep, line{now, text})
	return before
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
