package followup

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for s, want := range map[string]bool{"YES": true, "yes.": true, " Yes, ask": true, "NO": false, "no": false, "": false, "I think YES": false} {
		if got := Parse(s); got != want {
			t.Errorf("Parse(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestChain(t *testing.T) {
	var c Chain
	t0 := time.Unix(1_000_000, 0)
	if c.Begin("a", t0) {
		t.Fatal("Begin with no armed follow-up consumed one")
	}
	for i := 1; i <= Max; i++ {
		if !c.Allow("a", Max) {
			t.Fatalf("Allow false after %d follow-ups", i-1)
		}
		now := t0.Add(time.Duration(i) * time.Minute)
		c.Arm("a", now)
		if !c.Begin("a", now.Add(Window)) {
			t.Fatalf("round %d: Begin at Window did not consume", i)
		}
	}
	if c.Count("a") != Max || c.Allow("a", Max) {
		t.Fatalf("after %d rounds: count %d, Allow %v", Max, c.Count("a"), c.Allow("a", Max))
	}
	if c.Count("b") != 0 {
		t.Fatal("chain leaks across ESNs")
	}

	if c.Begin("a", t0.Add(time.Hour)) || c.Count("a") != 0 {
		t.Fatalf("Begin with no armed follow-up: count %d, want reset to 0", c.Count("a"))
	}

	c.Arm("a", t0)
	if c.Begin("a", t0.Add(Window+time.Millisecond)) || c.Count("a") != 0 {
		t.Fatal("a follow-up armed longer than Window ago counted")
	}

	c.Arm("a", t0)
	c.Begin("a", t0.Add(time.Second))
	c.Reset("a")
	if c.Count("a") != 0 || c.Begin("a", t0.Add(2*time.Second)) {
		t.Fatal("Reset left a count or an armed follow-up")
	}
}
