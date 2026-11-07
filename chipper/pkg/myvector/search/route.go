// Package search pre-fetches live data (weather, web snippets) into the prompt's {search} block (plan
// 2026-11-07-web-search.md (docs/plans/)).
package search

import (
	"slices"
	"strings"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Class is what a sentence needs looked up; None never triggers a lookup.
type Class int

const (
	None Class = iota
	Weather
	Live
	Recent
)

func (c Class) String() string {
	return [...]string{"none", "weather", "live", "recent"}[c]
}

// table holds one language's cues: exact tokens, token prefixes (Estonian stems), and token phrases.
type table struct {
	exact, prefix []string
	phrases       [][]string
}

func (t table) has(toks []string) bool {
	for i, w := range toks {
		if slices.Contains(t.exact, w) {
			return true
		}
		for _, p := range t.prefix {
			if strings.HasPrefix(w, p) {
				return true
			}
		}
		for _, ph := range t.phrases {
			if i+len(ph) <= len(toks) && slices.Equal(toks[i:i+len(ph)], ph) {
				return true
			}
		}
	}
	return false
}

func words(s ...string) [][]string {
	out := make([][]string, len(s))
	for i, p := range s {
		out[i] = strings.Fields(p)
	}
	return out
}

type cues struct {
	weather, live, recent table
	// self: a first/second-person pronoun makes "kes on…"/"who is…" chit-chat, not Recent.
	self []string
}

var tables = map[string]cues{
	lang.Estonian: {
		// "ilm" forms are exact: "ilmselt" is not weather; bare "ilma" is handled in Route.
		weather: table{
			exact:  []string{"ilm", "ilmas", "ilmaga", "ilmast", "sajab", "sadama", "sadu", "lund", "lumi", "lume", "külm", "soe", "palav"},
			prefix: []string{"ilmateat", "ilmaprognoos", "prognoos", "vihm", "temperatuur", "kraadi", "tuul", "lumesadu", "päikesepaiste"},
		},
		live: table{
			exact: []string{"lahti", "avatud", "suletud", "maksab", "maksavad", "hind", "hinna", "hinnad", "bitcoin", "bitcoini"},
			prefix: []string{"uudis", "jalgpall", "korvpall", "jäähoki", "võitis", "võitsid", "tulemus", "skoor", "kurss", "kursi",
				"börs", "aktsia", "lahtiolek"},
		},
		recent: table{phrases: words("kes on", "kes oli", "mis juhtus", "mis toimub", "mis toimus")},
		self:   []string{"sa", "sina", "sinu", "su", "ma", "mina", "minu", "mu", "me", "meie", "sinuga"},
	},
	lang.English: {
		weather: table{exact: []string{"weather", "forecast", "rain", "raining", "rainy", "snow", "snowing", "temperature",
			"sunny", "umbrella", "windy", "degrees"}},
		live: table{
			exact: []string{"news", "headlines", "score", "scores", "won", "price", "prices", "cost", "costs", "bitcoin",
				"stock", "stocks", "open", "opens", "closes"},
			phrases: words("exchange rate", "opening hours"),
		},
		recent: table{phrases: words("who is", "who's", "who was", "what happened", "what's happening", "what is happening")},
		self:   []string{"you", "your", "i", "me", "my", "we", "us", "our"},
	},
}

// Route classifies the sentence by lang's cues (English for any other lang); Weather wins over Live over Recent.
func Route(sentence, l string) Class {
	c, ok := tables[l]
	if !ok {
		c = tables[lang.English]
	}
	toks := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(sentence, "’", "'")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
	switch {
	case c.weather.has(toks) || etIlma(l, toks):
		return Weather
	case c.live.has(toks):
		return Live
	case c.recent.has(toks) && !slices.ContainsFunc(toks, func(w string) bool { return slices.Contains(c.self, w) }):
		return Recent
	}
	return None
}

// etIlma: Estonian "ilma" is weather's genitive ("ilma prognoos") unless an abessive -ta word follows ("ilma suhkruta").
func etIlma(l string, toks []string) bool {
	if l != lang.Estonian {
		return false
	}
	for i, w := range toks {
		if w == "ilma" && (i+1 == len(toks) || !strings.HasSuffix(toks[i+1], "ta")) {
			return true
		}
	}
	return false
}
