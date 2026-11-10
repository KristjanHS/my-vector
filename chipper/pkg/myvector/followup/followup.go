// Package followup decides whether the robot listens again after an LLM answer and caps the chain
// (2026-11-07-follow-up-listening.md (docs/plans/) F1, F2).
package followup

import (
	"strings"
	"sync"
	"time"
)

const (
	// Prompt is the YES/NO decider's system message (followupprobe P1: prompts[0]).
	Prompt = "A voice robot just answered a person. Reply YES if the robot's reply asks the person a question or invites them to answer; otherwise reply NO. Reply with one word: YES or NO."
	// Max is the most follow-ups in a row per robot.
	Max = 10
	// MaxListening caps the chain while active listening is on: room talk gets at most 3 answers in a row (G1).
	MaxListening = 3
	// Window is how long an armed follow-up waits for its request: the 1/3 s DoNewRequest delay + the robot's 10 s streamingTimeout + margin.
	Window = 15 * time.Second
	// Timeout bounds one decider call; a later answer counts as NO.
	Timeout = 5 * time.Second
	// MaxTokens caps the decider's reply.
	MaxTokens = 8
	// ClaudeModel decides on the Claude brain.
	ClaudeModel = "claude-haiku-5-5"
	// NoSpeech: an armed follow-up whose clip's STT no_speech (max segment) reaches this is blank (G1, P4).
	NoSpeech = 0.6
	// WakeNoSpeech: an unarmed turn (wake or "I have a question") scored this high is a silent wake, dropped (silent-wake-gate S3).
	WakeNoSpeech = 0.8
)

// RuleModels are Ollama model-name prefixes (after any "namespace/") whose decider is Rule, not an LLM call
// (2026-11-07-follow-up-listening.md (docs/plans/) 6.4).
var RuleModels = []string{"apertus"}

// ByRule reports whether model decides by Rule.
func ByRule(model string) bool {
	m := strings.ToLower(model)
	m = m[strings.LastIndex(m, "/")+1:]
	for _, p := range RuleModels {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

// Rule is the no-LLM decider: YES iff the spoken reply contains a question mark.
func Rule(reply string) bool { return strings.Contains(reply, "?") }

var noSpeech sync.Map // ESN -> its last transcript's no_speech

// SetNoSpeech records esn's last transcript score (et-stt "no_speech").
func SetNoSpeech(esn string, p float64) { noSpeech.Store(esn, p) }

// TakeNoSpeech returns and clears esn's last score; 0 when none was recorded.
func TakeNoSpeech(esn string) float64 {
	v, _ := noSpeech.LoadAndDelete(esn)
	p, _ := v.(float64)
	return p
}

// User is the decider's user message for one exchange.
func User(ask, reply string) string { return "Person: " + ask + "\nRobot: " + reply }

// Parse is true only on a case-insensitive YES prefix.
func Parse(s string) bool { return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "YES") }

type state struct {
	count int
	armed time.Time // zero = no follow-up pending
}

// Chain counts consecutive follow-ups per ESN; the zero value is ready.
type Chain struct {
	mu sync.Mutex
	m  map[string]state
}

// Begin runs as a request starts: it consumes a follow-up armed within Window (count+1, true);
// anything else resets the count (false).
func (c *Chain) Begin(esn string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.get(esn)
	ok := !s.armed.IsZero() && !now.Before(s.armed) && now.Sub(s.armed) <= Window
	if ok {
		s.count++
	} else {
		s.count = 0
	}
	s.armed = time.Time{}
	c.m[esn] = s
	return ok
}

// Arm marks a follow-up as fired for esn at now.
func (c *Chain) Arm(esn string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.get(esn)
	s.armed = now
	c.m[esn] = s
}

// Allow is true while esn has fewer than max follow-ups in a row.
func (c *Chain) Allow(esn string, max int) bool { return c.Count(esn) < max }

// Count is esn's follow-ups in a row.
func (c *Chain) Count(esn string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.get(esn).count
}

// Reset ends esn's chain.
func (c *Chain) Reset(esn string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.get(esn)
	c.m[esn] = state{}
}

func (c *Chain) get(esn string) state {
	if c.m == nil {
		c.m = map[string]state{}
	}
	return c.m[esn]
}
