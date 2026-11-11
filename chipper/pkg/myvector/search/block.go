package search

import (
	"fmt"
	"net/url"
	"strings"
	"time"

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

// now is the clock shortDate reads the current year from; a var so tests pin it.
var now = time.Now

// dateForms are the provider date layouts shortDate parses (Tavily RFC 1123 / ISO, SearXNG ISO without a zone).
var dateForms = []string{time.RFC1123, time.RFC1123Z, "Mon, 2 Jan 2006 15:04:05 MST", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}

// shortDate is d as a short local date for an Ollama brain (OllamaEt "10.11", Ollama "Nov 10"; the year only when
// not this year); d unchanged when unparsed. Claude gets the raw date (N5: its block is unchanged).
func shortDate(brain, d string) string {
	for _, f := range dateForms {
		t, err := time.ParseInLocation(f, strings.TrimSpace(d), time.Local)
		if err != nil {
			continue
		}
		t = t.In(time.Local)
		old := t.Year() != now().Year()
		switch {
		case brain == prompt.OllamaEt && old:
			return fmt.Sprintf("%d.%d.%d", t.Day(), int(t.Month()), t.Year())
		case brain == prompt.OllamaEt:
			return fmt.Sprintf("%d.%d", t.Day(), int(t.Month()))
		case old:
			return t.Format("Jan 2, 2006")
		}
		return t.Format("Jan 2")
	}
	return d
}

func render(brain string, rs []Result, runes int) string {
	var b strings.Builder
	b.WriteString(prompt.SearchWrap(brain))
	for _, r := range rs {
		src := r.URL
		if u, err := url.Parse(r.URL); err == nil && u.Host != "" {
			src = strings.TrimPrefix(u.Host, "www.")
		}
		if r.Date != "" && src != "" {
			src += ", "
		}
		if prompt.IsOllama(brain) {
			src += shortDate(brain, r.Date)
		} else {
			src += r.Date
		}
		fmt.Fprintf(&b, "\n- %s", clip(r.Title, titleRunes))
		if src != "" {
			fmt.Fprintf(&b, " (%s)", clip(src, titleRunes))
		}
		fmt.Fprintf(&b, ": %s", clip(r.Snippet, runes))
	}
	return b.String()
}

// Block is the {search} block for brain: prompt.SearchWrap, then its first Snippets results, the last dropped until the block fits
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
		if b := render(brain, rs[:k], runes); prompt.Estimate(b) <= prompt.SearchBudget(brain) {
			return b
		}
	}
	return ""
}
