package search

import (
	"strings"
	"testing"

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
		for brain, want := range map[string]int{prompt.Ollama: 3, prompt.Claude: 5} {
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
