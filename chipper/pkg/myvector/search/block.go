package search

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// Result is one web search hit.
type Result struct{ Title, URL, Snippet, Date string }

// Snippets per brain and their rune caps (plan W9); titles are capped at titleRunes.
const (
	SnippetsOllama, SnippetsClaude = 3, 5
	runesOllama, runesClaude       = 160, 360
	titleRunes                     = 60
)

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

func render(rs []Result, runes int) string {
	var b strings.Builder
	b.WriteString("Live web results for the user's question:")
	for _, r := range rs {
		src := r.URL
		if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
			src = strings.TrimPrefix(u.Host, "www.")
		}
		if r.Date != "" && src != "" {
			src += ", "
		}
		src += r.Date
		fmt.Fprintf(&b, "\n- %s", clip(r.Title, titleRunes))
		if src != "" {
			fmt.Fprintf(&b, " (%s)", clip(src, titleRunes))
		}
		fmt.Fprintf(&b, ": %s", clip(r.Snippet, runes))
	}
	return b.String()
}

// Block is the {search} block for brain: its first Snippets results, the last dropped until the block fits
// prompt.SearchBudget; "" when none fit.
func Block(brain string, rs []Result) string {
	n, runes := SnippetsClaude, runesClaude
	if prompt.IsOllama(brain) {
		n, runes = SnippetsOllama, runesOllama
	}
	if len(rs) > n {
		rs = rs[:n]
	}
	for k := len(rs); k > 0; k-- {
		if b := render(rs[:k], runes); prompt.Estimate(b) <= prompt.SearchBudget(brain) {
			return b
		}
	}
	return ""
}
