package followup

import (
	"strings"
	"unicode"
)

// RepeatMin is G2's word overlap at which a closing question repeats an earlier one (8.1; 0.80 is the one allowed raise).
const RepeatMin = 0.75

// words is s's distinct words: lowercase, punctuation stripped.
func words(s string) map[string]bool {
	w := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		w[f] = true
	}
	return w
}

// Overlap is the share of the longer sentence's distinct words that both sentences hold.
func Overlap(a, b string) float64 {
	wa, wb := words(a), words(b)
	n := len(wa)
	if len(wb) > n {
		n = len(wb)
	}
	if n == 0 {
		return 0
	}
	common := 0
	for w := range wa {
		if wb[w] {
			common++
		}
	}
	return float64(common) / float64(n)
}

// Questions is the '?'-ending sentences of an assistant message.
func Questions(msg string) []string {
	var out []string
	start := 0
	rs := []rune(msg)
	for i, r := range rs {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(rs) || unicode.IsSpace(rs[i+1])) {
			if r == '?' {
				out = append(out, strings.TrimSpace(string(rs[start:i+1])))
			}
			start = i + 1
		}
	}
	return out
}

// Repeats reports whether sentence s is a question with Overlap ≥ min against a question in the earlier assistant messages said.
func Repeats(s string, said []string, min float64) bool {
	if !strings.HasSuffix(strings.TrimSpace(s), "?") {
		return false
	}
	for _, m := range said {
		for _, q := range Questions(m) {
			if Overlap(s, q) >= min {
				return true
			}
		}
	}
	return false
}
