package memory

import (
	"sort"
	"strings"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// Per-brain counts for {facts} and {diary}.
const (
	FactsOllama = 3
	FactsClaude = 30
	DiaryOllama = 1
	DiaryClaude = 7
)

// Block renders esn's {facts} (profile + active facts ranked against utterance) and {diary} (newest entries first),
// dropping diary then facts from the end until both fit prompt.MemoryBudget(brain); the profile is never dropped.
func Block(s *Store, esn, utterance, brain string) (facts, diary string) {
	nFacts, nDiary := FactsClaude, DiaryClaude
	if prompt.IsOllama(brain) {
		nFacts, nDiary = FactsOllama, DiaryOllama
	}
	fs := Retrieve(s, esn, utterance)
	if len(fs) > nFacts {
		fs = fs[:nFacts]
	}
	var es []Entry
	for i := len(s.Diary) - 1; i >= 0 && len(es) < nDiary; i-- {
		if e := s.Diary[i]; e.ESN == esn && e.Status != Removed {
			es = append(es, e)
		}
	}
	profile := s.Profiles[esn].Text
	for {
		facts, diary = renderFacts(profile, fs), renderDiary(es)
		if prompt.Estimate(facts)+prompt.Estimate(diary) <= prompt.MemoryBudget(brain) {
			return facts, diary
		}
		switch {
		case len(es) > 0:
			es = es[:len(es)-1]
		case len(fs) > 0:
			fs = fs[:len(fs)-1]
		default:
			return facts, diary
		}
	}
}

func renderFacts(profile string, fs []Fact) string {
	if profile == "" && len(fs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("What you remember about the user:")
	if profile != "" {
		b.WriteString("\n" + profile)
	}
	for _, f := range fs {
		b.WriteString("\n- " + f.Text)
	}
	return b.String()
}

func renderDiary(es []Entry) string {
	if len(es) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Your diary of recent days:")
	for _, e := range es {
		b.WriteString("\n- " + e.Day + ": " + e.Text)
	}
	return b.String()
}

// Retrieve is esn's active facts, most 5-rune-prefix tokens shared with utterance first, newer learned_at on a tie.
func Retrieve(s *Store, esn, utterance string) []Fact {
	want := prefixes(utterance)
	type scored struct {
		f Fact
		n int
	}
	var out []scored
	for _, f := range s.Facts {
		if f.ESN != esn || f.Status != Active {
			continue
		}
		have, n := prefixes(f.Text+" "+f.Evidence), 0
		for p := range want {
			if have[p] {
				n++
			}
		}
		out = append(out, scored{f, n})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].f.LearnedAt.After(out[j].f.LearnedAt)
	})
	fs := make([]Fact, len(out))
	for i, o := range out {
		fs[i] = o.f
	}
	return fs
}

// prefixes is the set of lower-cased 5-rune prefixes of s's words of 3+ runes.
func prefixes(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		r := []rune(w)
		if len(r) < 3 {
			continue
		}
		if len(r) > 5 {
			r = r[:5]
		}
		set[string(r)] = true
	}
	return set
}
