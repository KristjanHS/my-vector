package listen

import (
	"slices"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

func TestMatchTable(t *testing.T) {
	on := []string{"hey vector, activate active listening", "Activate active listening!", "vector active listening on please",
		"Hei Vektor, aktiveeri aktiivne kuulamine.", "palun aktiivne kuulamine sees", "Heivektor, aktiveeri aktiivne kuulamine.", "aktiveerige aktiivne kuulamine.",
		"Hey Vector, turn on active listening.", "activate the active listening", "turn active listening on",
		"activate active listering"}
	off := []string{"hey vector, deactivate active listening", "active listening off.", "Hei Vektor, deaktiveeri, aktiivne kuulamine!",
		"Ei, vektor, aktiivne kuulamine välja.", "aktiivne kuulamine välja", "deaktiveerige aktiivne kuulamine",
		"hey vector, turn off active listening", "deactivate the active listening", "turn active listening off",
		"deaktiveeriv aktiivne kuulamine", "deactivate active listering"}
	misses := []string{"activate the lights", "listening to music", "ära aktiveeri aktiivne kuulamine",
		"the active listening off switch", "ei aktiveeri aktiivne kuulamine", "can you activate active listening for me",
		"active listening", "", "shut up", "don't turn on active listening", "turn on the lights", "turn off the lights",
		"I was actively listening to the radio and then turned it off", "aktiivne kuulamine see", "activate active lists"}
	for _, s := range on {
		if isOn, ok := Match(s); !ok || !isOn {
			t.Errorf("Match(%q) = %v %v, want on", s, isOn, ok)
		}
	}
	for _, s := range off {
		if isOn, ok := Match(s); !ok || isOn {
			t.Errorf("Match(%q) = %v %v, want off", s, isOn, ok)
		}
	}
	for _, s := range misses {
		if isOn, ok := Match(s); ok {
			t.Errorf("Match(%q) = hit (on=%v), want miss", s, isOn)
		}
	}
}

func at(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, time.Local) }

func TestStateAutoOff(t *testing.T) {
	var logs []string
	s := NewState()
	s.Log = func(l string) { logs = append(logs, l) }

	s.Activate("a", at(7, 12, 0))
	s.Heard("a", at(7, 12, 25))
	if !s.Active("a", at(7, 12, 54)) {
		t.Fatal("off 29 min after last utterance, want on")
	}
	if s.Active("a", at(7, 12, 55)) {
		t.Fatal("on 30 min after last utterance, want idle off")
	}

	s.Activate("b", at(7, 20, 50))
	s.Heard("b", at(7, 20, 59))
	if s.Active("b", at(7, 21, 1)) {
		t.Fatal("on after 21:00 crossed, want night off")
	}

	s.Activate("c", at(7, 22, 0))
	if !s.Active("c", at(7, 22, 10)) {
		t.Fatal("activated inside the night window is turned off, want on until idle")
	}
	s.Activate("d", at(8, 8, 0))
	s.Heard("d", at(8, 9, 0))
	if !s.Active("d", at(8, 9, 20)) {
		t.Fatal("morning activation crossed no 21:00, want on")
	}

	s.Activate("e", at(7, 12, 0))
	s.Deactivate("e", "phrase")
	s.Heard("e", at(7, 12, 1))
	if s.Active("e", at(7, 12, 2)) || !s.LastSpoke("e").IsZero() {
		t.Fatal("Heard revived a deactivated robot")
	}
	if len(logs) != 3 || logs[0] != "myvector listen: off (idle)" || logs[1] != "myvector listen: off (night)" {
		t.Fatalf("logs = %q, want idle, night, phrase", logs)
	}
	s.Deactivate("e", "again")
	if len(logs) != 3 {
		t.Fatal("deactivating an off robot logged")
	}
}

func TestActivateTwiceKeepsState(t *testing.T) {
	s := NewState()
	s.Activate("a", at(7, 12, 0))
	s.Spoke("a", at(7, 12, 1))
	s.Spoken("a", at(7, 12, 1), at(7, 12, 2))
	if c := s.claim("a", at(7, 12, 1), true); c != "" {
		t.Fatalf("claim = %q, want the slot", c)
	}
	s.Activate("a", at(7, 12, 3))
	b := s.bots["a"]
	if !b.busy || b.streak != 1 || !s.Echo("a", at(7, 12, 1), at(7, 12, 2)) || !b.since.Equal(at(7, 12, 3)) {
		t.Fatalf("second Activate: busy=%v streak=%d since=%v echo=%v, want busy, 1, 12:03, echo kept",
			b.busy, b.streak, b.since, s.Echo("a", at(7, 12, 1), at(7, 12, 2)))
	}
}

// quiet.Match runs first, so an exact overlap would make quiet silently win.
func TestNoQuietOverlap(t *testing.T) {
	for _, p := range append(slices.Clone(On), Off...) {
		if slices.Contains(quiet.Phrases, p) {
			t.Errorf("%q is also a quiet phrase", p)
		}
	}
}
