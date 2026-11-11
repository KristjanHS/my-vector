package search

import (
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func TestSearchBudget(t *testing.T) {
	for _, word := range []string{"tulemused ", "õõõõäääüü ", "result ab "} { // et, et with diacritics, en
		long := strings.Repeat(word, 80) // 800 runes: past every cap
		// Realistic: a 60-rune title, a 25-rune host, a max-length snippet. The pinned count is the W9 guard:
		// Block drops results until Estimate ≤ SearchBudget, so that check alone cannot fail.
		fit := make([]Result, 6)
		huge := make([]Result, 6)
		for i := range fit {
			fit[i] = Result{Title: string([]rune(long)[:60]), URL: "https://www.uudised.example-meedia.ee/a/" + long, Snippet: long, Date: "2026-11-07"}
			huge[i] = Result{Title: long, URL: "https://www.example-uudised.ee/" + long, Snippet: long, Date: "2026-11-07"}
		}
		for brain, want := range map[string]int{prompt.Ollama: 3, prompt.OllamaEt: 3, prompt.Claude: 5} {
			b := Block(brain, fit)
			if n := strings.Count(b, "\n- "); n != want {
				t.Errorf("%s %q realistic: %d snippets (Estimate %d, budget %d), want %d", brain, word, n, prompt.Estimate(b), prompt.SearchBudget(brain), want)
			}
			b = Block(brain, huge)
			if e := prompt.Estimate(b); e > prompt.SearchBudget(brain) || !strings.Contains(b, "\n- ") {
				t.Errorf("%s %q max-length: Estimate %d, budget %d, block %q", brain, word, e, prompt.SearchBudget(brain), b)
			}
		}
	}
	for _, c := range []struct {
		r    Result
		want string
	}{
		{Result{Title: "Riigikogu", URL: "https://www.err.ee/x", Snippet: "Täna  hääletati\neelarve.", Date: "7.11"}, "- Riigikogu (err.ee, 7.11): Täna hääletati eelarve."},
		{Result{Title: "Riigikogu", Snippet: "Eelarve."}, "- Riigikogu: Eelarve."},
		{Result{Title: "Riigikogu", Snippet: "Eelarve.", Date: "7.11"}, "- Riigikogu (7.11): Eelarve."},
	} {
		if b := Block(prompt.Claude, []Result{c.r}); b != "Live web results for the user's question:\n"+c.want {
			t.Errorf("Block(%+v) = %q", c.r, b)
		}
	}
	if b := Block(prompt.Ollama, nil); b != "" {
		t.Errorf("no results: Block = %q", b)
	}
}

// The Ollama brains get the usage rule in their language ahead of the hits; Claude keeps the plain header (N5).
func TestBlockWrap(t *testing.T) {
	pinClock(t)
	r := []Result{{Title: "Börs", URL: "https://www.err.ee/x", Snippet: "Aktsiad tõusid.", Date: "2026-11-10"}}
	for brain, want := range map[string][]string{
		prompt.OllamaEt: {"Värsked veebitulemused", "nimeta allikas ja kuupäev", "Ära ütle, et sa ei saa otsida", "(err.ee, 10.11)"},
		prompt.Ollama:   {"Fresh web results", "naming the source and date", "Never say you can't search", "(err.ee, Nov 10)"},
		prompt.Claude:   {"Live web results for the user's question:\n- Börs (err.ee, 2026-11-10)"},
	} {
		b := Block(brain, r)
		for _, w := range want {
			if !strings.Contains(b, w) {
				t.Errorf("%s: Block = %q, lacks %q", brain, b, w)
			}
		}
	}
}

func TestShortDate(t *testing.T) {
	pinClock(t)
	for _, c := range []struct{ brain, in, want string }{
		{prompt.OllamaEt, "2026-11-07", "7.11"},
		{prompt.OllamaEt, "2026-11-06T10:00:00", "6.11"},
		{prompt.OllamaEt, "Tue, 10 Nov 2026 12:00:00 GMT", "10.11"},
		{prompt.OllamaEt, "2019-03-02", "2.3.2019"},
		{prompt.Ollama, "2026-11-07", "Nov 7"},
		{prompt.Ollama, "2019-03-02", "Mar 2, 2019"},
		{prompt.OllamaEt, "Tue, 3 Nov 2026 12:00:00 GMT", "3.11"},
		{prompt.Ollama, "7.11", "7.11"},
		{prompt.Ollama, "eile", "eile"},
	} {
		if got := shortDate(c.brain, c.in); got != c.want {
			t.Errorf("shortDate(%s, %q) = %q, want %q", c.brain, c.in, got, c.want)
		}
	}
}

// pinClock fixes shortDate's year (2026) and zone (UTC+2, the robot's) so dates render the same on any host.
func pinClock(t *testing.T) {
	t.Helper()
	oldNow, oldLocal := now, time.Local
	time.Local = time.FixedZone("EET", 2*3600)
	now = func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.Local) }
	t.Cleanup(func() { now, time.Local = oldNow, oldLocal })
}
