package search

import (
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func TestSearchBudget(t *testing.T) {
	for _, word := range []string{"tulemused ", "õõõõäääüü ", "result ab "} { // et, et with diacritics, en
		long := strings.Repeat(word, 80) // 800 runes: past both caps
		rs := make([]Result, 6)
		for i := range rs {
			rs[i] = Result{Title: long, URL: "https://www.example-uudised.ee/" + long, Snippet: long, Date: "2026-11-07"}
		}
		for brain, max := range map[string]int{prompt.Ollama: SnippetsOllama, prompt.Claude: SnippetsClaude} {
			b := Block(brain, rs)
			n := strings.Count(b, "\n- ")
			if e := prompt.Estimate(b); e > prompt.SearchBudget(brain) || n == 0 || n > max {
				t.Errorf("%s %q: %d snippets, Estimate %d, budget %d", brain, word, n, e, prompt.SearchBudget(brain))
			}
		}
	}
	if b := Block(prompt.Claude, []Result{{Title: "Riigikogu", URL: "https://www.err.ee/x", Snippet: "Täna  hääletati\neelarve.", Date: "7.11"}}); b != "Live web results for the user's question:\n- Riigikogu (err.ee, 7.11): Täna hääletati eelarve." {
		t.Errorf("Block = %q", b)
	}
	if b := Block(prompt.Ollama, nil); b != "" {
		t.Errorf("no results: Block = %q", b)
	}
}
