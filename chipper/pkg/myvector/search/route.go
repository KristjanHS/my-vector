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

// gap in a phrase matches 0..maxGap tokens ("is * open" = "is rimi open").
const (
	gap    = "*"
	maxGap = 4
)

// table holds cues: exact tokens, token prefixes (Estonian stems), and token phrases (with gap wildcards).
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
			if matchAt(toks[i:], ph) {
				return true
			}
		}
	}
	return false
}

// matchAt: toks starts with phrase ph, each gap standing for 0..maxGap tokens.
func matchAt(toks, ph []string) bool {
	if len(ph) == 0 {
		return true
	}
	if ph[0] == gap {
		for k := 0; k <= maxGap && k <= len(toks); k++ {
			if matchAt(toks[k:], ph[1:]) {
				return true
			}
		}
		return false
	}
	return len(toks) > 0 && toks[0] == ph[0] && matchAt(toks[1:], ph[1:])
}

func words(s ...string) [][]string {
	out := make([][]string, len(s))
	for i, p := range s {
		out[i] = strings.Fields(p)
	}
	return out
}

// cues is one language's router. weather/live fire alone; weatherWeak (words with a non-weather sense: "soe
// tee", "snow white") fires only beside a weatherCtx token; liveWeak and recent never fire beside a self
// pronoun ("can you open your eyes", "kes on sinu lemmik"). recent also needs minRecent non-deictic tokens
// after its phrase ("kes on ta", "who's there", "mis juhtus?" are chit-chat).
type cues struct {
	weather, weatherWeak, live, liveWeak, recent table
	weatherCtx, self, deictic                    []string
}

const minRecent = 2

var tables = map[string]cues{
	lang.Estonian: {
		// "ilm" forms are exact: "ilmselt" is not weather; bare "ilma" is handled in Route. vihm/tuul are form
		// lists, not prefixes: "vihmauss", "vihmavari", "tuuleveski", "Tuuli" are not weather.
		weather: table{
			exact: []string{"ilm", "ilmas", "ilmaga", "ilmast", "sajab", "vihm", "vihma", "vihmane", "vihmaseks",
				"vihmast", "vihmaga", "vihmasadu", "vihmasaju", "tuul", "tuult", "tuulega", "tuuline", "tuulist",
				"tuuliseks", "tuulekiirus"},
			prefix: []string{"ilmateat", "ilmaprognoos", "lumesadu", "lumesaju", "päikesepaiste"},
		},
		weatherWeak: table{exact: []string{"soe", "sooja", "soojem", "soojaks", "külm", "külma", "külmem", "külmaks",
			"palav", "palavat", "palavaks", "lumi", "lund", "lume", "sadama", "sadu", "kraad", "kraadi", "kraade",
			"kraadid", "kraadini", "temperatuur", "temperatuuri", "prognoos", "prognoosi", "prognoosid"}},
		weatherCtx: []string{"täna", "tänane", "homme", "homne", "ülehomme", "õues", "väljas", "õhtul", "hommikul",
			"öösel", "päeval", "nädalavahetusel", "nädalavahetuseks"},
		live: table{
			exact:  []string{"bitcoin", "bitcoini"},
			prefix: []string{"uudis", "lahtiolek"},
		},
		// "kursis"/"tulemuseks" are not rates/results; lahti/kinni/maksab only in a question frame ("võta see
		// lahti", "see maksab kätte").
		liveWeak: table{
			exact: []string{"hind", "hinna", "hinnad", "hinda", "võitis", "võitsid", "kurss", "kursi", "kurssi",
				"kursid", "valuutakurss", "vahetuskurss", "tulemus", "tulemused", "tulemusi", "tulemuse"},
			prefix: []string{"jalgpall", "korvpall", "jäähoki", "skoor", "börs", "aktsia"},
			phrases: words("millal * lahti", "millal * kinni", "millal * avatakse", "millal * suletakse",
				"kas * lahti", "kas * kinni", "kas * avatud", "kas * suletud", "mis kell * lahti", "mis kell * kinni",
				"kui palju * maksab", "kui palju * maksavad", "mis * maksab", "mis * maksavad"),
		},
		recent: table{phrases: words("kes on", "kes oli", "mis juhtus", "mis toimub", "mis toimus")},
		self: []string{"sa", "sina", "sinu", "su", "sind", "sul", "sulle", "sinuga", "sinust", "ma", "mina", "minu",
			"mu", "mind", "mul", "mulle", "minuga", "minust", "me", "meie", "meil", "meile", "te", "teie", "teil"},
		deictic: []string{"ta", "tema", "see", "too", "seal", "siin", "nad", "nemad"},
	},
	lang.English: {
		weather: table{exact: []string{"weather", "forecast", "raining", "rainy", "snowing", "sunny", "windy"}},
		weatherWeak: table{exact: []string{"rain", "snow", "temperature", "degrees", "hot", "cold", "warm",
			"freezing", "umbrella"}},
		weatherCtx: []string{"today", "tomorrow", "tonight", "outside", "outdoors", "weekend", "morning",
			"afternoon", "evening"},
		live: table{
			exact:   []string{"news", "headlines", "bitcoin"},
			phrases: words("exchange rate", "opening hours"),
		},
		// won/open/cost only in a question frame ("i won the game", "open the door", "what does it cost you").
		liveWeak: table{
			exact: []string{"price", "prices", "score", "scores", "results", "stock", "stocks"},
			phrases: words("who won", "is * open", "is * closed", "are * open", "are * closed",
				"when does * open", "when does * close", "when do * open", "when do * close",
				"what time does * open", "what time does * close", "how much does * cost", "how much do * cost",
				"cost of"),
		},
		recent: table{phrases: words("who is", "who's", "who was", "what happened", "what's happening", "what is happening")},
		self: []string{"you", "your", "yours", "yourself", "i", "me", "my", "mine", "myself", "we", "us", "our",
			"ours", "i'm", "i've", "i'd", "i'll", "you're", "you've", "you'd", "you'll", "we're", "we've", "we'd",
			"we'll"},
		deictic: []string{"he", "she", "this", "that", "it", "there", "him", "her", "they", "them"},
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
	in := func(set []string) bool {
		return slices.ContainsFunc(toks, func(w string) bool { return slices.Contains(set, w) })
	}
	self := in(c.self)
	switch {
	case c.weather.has(toks) || etIlma(l, toks) || !self && c.weatherWeak.has(toks) && in(c.weatherCtx):
		return Weather
	case c.live.has(toks) || !self && c.liveWeak.has(toks):
		return Live
	case !self && c.recentAsk(toks):
		return Recent
	}
	return None
}

// recentAsk: a recent phrase followed by at least minRecent non-deictic tokens ("kes on Eesti peaminister",
// not "kes on ta" or a bare name like "kes on Tuuli").
func (c cues) recentAsk(toks []string) bool {
	for i := range toks {
		for _, ph := range c.recent.phrases {
			if !matchAt(toks[i:], ph) {
				continue
			}
			n := 0
			for _, w := range toks[i+len(ph):] {
				if !slices.Contains(c.deictic, w) {
					n++
				}
			}
			if n >= minRecent {
				return true
			}
		}
	}
	return false
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
