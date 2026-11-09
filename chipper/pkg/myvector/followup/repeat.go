package followup

import (
	"strings"
	"unicode"
)

// RepeatMin is G2's word overlap at which a closing question repeats an earlier one (8.1; 0.80 is the one allowed raise).
const RepeatMin = 0.75

// RepeatWords: a question with fewer words ("Mida?") never drops (user ruling 11-09).
const RepeatWords = 4

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

// closers may follow a sentence's end mark before the space (forkSplitSentence ends a sentence after `."` too).
const closers = `"'”»)`

// question reports whether sentence s ends in a mark run holding '?' ("?", "?!", `?"`).
func question(s string) bool {
	s = strings.TrimRight(s, closers+" \t\n")
	end := strings.TrimRight(s, ".!?")
	return strings.Contains(s[len(end):], "?")
}

// Questions is the question sentences of an assistant message; a sentence ends at a .!? run, closers, then space or the end.
func Questions(msg string) []string {
	var out []string
	rs := []rune(msg)
	start := 0
	for i := 0; i < len(rs); i++ {
		if !strings.ContainsRune(".!?", rs[i]) {
			continue
		}
		j := i
		for j < len(rs) && strings.ContainsRune(".!?"+closers, rs[j]) {
			j++
		}
		if j < len(rs) && !unicode.IsSpace(rs[j]) {
			i = j - 1
			continue
		}
		if s := strings.TrimSpace(string(rs[start:j])); question(s) {
			out = append(out, s)
		}
		start, i = j, j-1
	}
	return out
}

// Repeats reports whether sentence s is a question of ≥ RepeatWords words with Overlap ≥ min against a question in the earlier assistant messages said.
func Repeats(s string, said []string, min float64) bool {
	if !question(s) || len(words(s)) < RepeatWords {
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
