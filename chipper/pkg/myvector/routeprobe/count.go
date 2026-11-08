package main

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// counts is one reply's W, L, K, H counters (et-prompt-tuning.md (docs/plans/) §Metrics).
type counts struct {
	words              int
	long, list         bool
	self, leak, recall bool
}

var (
	cmdRe  = regexp.MustCompile(`\{\{[^}]*\}\}`)
	listRe = regexp.MustCompile(`(?m)^\s*([-*•]\s|\d{1,2}[.)]\s+\p{Lu}|Samm \d+)|\*\*|(?m)^\s*#{1,6}\s`)
)

// count is the counters of answer (cleaned) and raw (as sent) to a; tokens are the profile tokens, minus those in the ask.
func count(a ask, raw, answer string, tokens []string) counts {
	answer = cmdRe.ReplaceAllString(answer, " ")
	c := counts{words: len(strings.Fields(answer)), long: vars.ForkLongAsk(a.Text), self: a.Kind == "self",
		list: listRe.MatchString(cmdRe.ReplaceAllString(raw, " "))}
	inAsk := hits(a.Text, tokens)
	var tok []string
	for _, t := range tokens {
		if !slices.Contains(inAsk, t) {
			tok = append(tok, t)
		}
	}
	hit := len(hits(answer, tok)) > 0
	c.leak, c.recall = hit && !c.self, hit && c.self
	return c
}

// countLine is cs's "words m/p90 long m/p90 · list % · leak % (n) · recall % (n)".
func countLine(cs []counts) string {
	var short, long []int
	var list, leak, nLeak, recall, nSelf int
	for _, c := range cs {
		if c.long {
			long = append(long, c.words)
		} else {
			short = append(short, c.words)
		}
		if c.list {
			list++
		}
		if c.self {
			nSelf++
			if c.recall {
				recall++
			}
		} else {
			nLeak++
			if c.leak {
				leak++
			}
		}
	}
	return fmt.Sprintf("words %s long %s · list %s · leak %s (n %d) · recall %s (n %d)",
		medP90(short), medP90(long), pct(list, len(cs)), pct(leak, nLeak), nLeak, pct(recall, nSelf), nSelf)
}

// medP90 is xs's nearest-rank median/p90, "-" when empty.
func medP90(xs []int) string {
	if len(xs) == 0 {
		return "-"
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	at := func(p float64) int { return s[int(math.Ceil(p*float64(len(s))))-1] }
	return fmt.Sprintf("%d/%d", at(0.5), at(0.9))
}

func pct(k, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(k)/float64(n))
}
