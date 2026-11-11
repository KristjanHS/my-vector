package search

import (
	"strings"
	"testing"
	"unicode/utf8"
)

const turn1 = "otsin semikontaktorstide teemalisi finantsuudiseid, mis mõjutavad aktsiaturve."

func TestKeywords(t *testing.T) {
	for in, want := range map[string]string{
		turn1: "semikontaktorstide finantsuudiseid mõjutavad aktsiaturve",
		"otsi uudiseid seoses sellega, mis sa just rääkisid.": "uudiseid",
		"search for present news about semiconductors":        "present news semiconductors",
		"kas sa saad otsida":              "kas sa saad otsida",
		"what is the google stock price":  "google stock price",
		"latest US stock market news":     "latest us stock market news",
		"internet outage in estonia":      "internet outage estonia",
		"can you google the nvidia price": "nvidia price",
		"palun otsi Nvidia uudiseid":      "nvidia uudiseid",
	} {
		if got := Keywords(in); got != want {
			t.Errorf("Keywords(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnglish(t *testing.T) {
	got := English(Keywords(turn1))
	for _, w := range []string{"semiconductor", "financial news", "stock market"} {
		if !strings.Contains(got, w) {
			t.Errorf("English(turn 1) = %q, lacks %q", got, w)
		}
	}
	if got := English("pooljuhtide aktsiad uudised Nvidia"); got != "semiconductor stocks news Nvidia" {
		t.Errorf("English = %q (stems map, a name passes through)", got)
	}
	if got := English("finantsuudised uudised"); got != "financial news news" {
		t.Errorf("English = %q, want the longest stem first", got)
	}
	for in, want := range map[string]string{
		// short stems are form lists: a longer word sharing the prefix passes through
		"tänava": "tänava", "tänan": "tänan", "kursis": "kursis", "hindamine": "hindamine", "hinded": "hinded",
		"hinnang": "hinnang", "turundus": "turundus",
		"turul": "market", "hinnad": "price", "tänased": "today",
	} {
		if got := English(in); got != want {
			t.Errorf("English(%q) = %q, want %q", in, got, want)
		}
	}
}

// A prefix stem of maxStemRunes or fewer misfires ("täna" → "tänava"); it belongs in forms.
func TestLexiconStemsLong(t *testing.T) {
	for st := range lexicon {
		if utf8.RuneCountInString(st) <= maxStemRunes {
			t.Errorf("lexicon stem %q has <= %d runes; list its forms in forms", st, maxStemRunes)
		}
	}
}

func TestIsNews(t *testing.T) {
	for s, want := range map[string]bool{
		turn1:                                true,
		"latest news":                        true,
		"kuidas läheb börsil?":               true,
		"semiconductor stocks today":         true,
		"mis on euro kurss?":                 false,
		"kes on Eesti peaminister?":          false,
		"what's the price of bitcoin":        false,
		"is the christmas market open today": false,
		"is the ps5 in stock at euronics":    false,
		"latest stock market news":           true,
		"how is the stock market doing":      true,
	} {
		if got := IsNews(s); got != want {
			t.Errorf("IsNews(%q) = %v, want %v", s, got, want)
		}
	}
}

// incident is the 2026-11-11 08:27 turn 1 (search-trigger-fixes F5).
const incident = "search for present news about semiconductors related financial."

func TestFollowQuery(t *testing.T) {
	for _, c := range []struct {
		sentence string
		prev     []string
		want     string // "" = nothing carried
	}{
		{"otsi viimase pakkumise pealt, number kolm.", []string{incident}, "present news semiconductors financial pakkumise pealt"},
		{"otsi seda", []string{incident}, "present news semiconductors financial"},
		{"search it", []string{incident}, "present news semiconductors financial"},
		{"otsi seda", []string{"aitäh", incident}, "present news semiconductors financial"},
		{"otsi seda", []string{"aitäh", "kuidas sul läheb?", incident}, ""},
		{"otsi seda", []string{"kuidas sul läheb?"}, ""},
		{"otsi seda", nil, ""},
		{"otsi ilma Tartus", []string{incident}, ""},
		{"find news about Tesla this week", []string{incident}, ""},
		{"search what time it is in Tokyo", []string{incident}, ""},
		{"search for that movie with Tom Hanks", []string{incident}, ""},
		{"otsi viimase kuu uudised", []string{"otsi Nvidia aktsia hinda"}, ""},
		{"google the phone number of Tallinn city hall", []string{incident}, ""},
		{"search Lagarde number two", []string{"latest news about the European Central Bank interest rate decision"}, "latest news european central bank lagarde"},
		{"otsi rohkem", []string{"otsi seda", incident}, "present news semiconductors financial rohkem"},
	} {
		q, topic := followQuery(c.sentence, c.prev, "et-EE")
		if c.want == "" {
			if topic != "" || q.Text != Keywords(c.sentence) {
				t.Errorf("%q after %q: carried %q (query %q), want none", c.sentence, c.prev, topic, q.Text)
			}
			continue
		}
		if q.Text != c.want || !q.News || topic == "" {
			t.Errorf("%q after %q: query %q news %v topic %q, want %q news", c.sentence, c.prev, q.Text, q.News, topic, c.want)
		}
	}
}

func TestRelevant(t *testing.T) {
	junk := []Result{ // the 2026-11-11 turn-2 set (search-news-quality incident)
		{Title: "Räägime asjast", Snippet: "YouTube"},
		{Title: "Uudis – Vikitsitaadid", Snippet: "Uudis on teade."},
		{Title: "ERR", Snippet: "Eesti Rahvusringhäälingu uudised"},
	}
	semi := []Result{ // block1: santec names no query word, so it goes
		{Title: "Mitsui Chemicals acquires JSR", Snippet: "a major semiconductor materials manufacturer"},
		{Title: "santec (6777) surges +23.8%!", Snippet: "Tokyo market, Nikkei 225"},
		{Title: "10 Stocks Analysts Just Upgraded", Snippet: "Brent crude rose"},
	}
	cases := []struct {
		name, q string
		rs      []Result
		want    string
	}{
		{"turn-2 junk, topic query", "finantsuudis aktsiatur", junk, ""},
		{"cue words only keep all", "uudis", junk, "Räägime asjast|Uudis – Vikitsitaadid|ERR"},
		{"time words are cues", "tänased uudised today week viimane", junk, "Räägime asjast|Uudis – Vikitsitaadid|ERR"},
		{"inflected form starts its base", "lätis", []Result{{Title: "Läti valitsus"}, {Title: "Leedu"}}, "Läti valitsus"},
		{"possessive dropped", "fed's", []Result{{Title: "Fed holds rates"}, {Title: "Feast"}}, "Fed holds rates"},
		{"one off-topic result dropped", "semiconductor stocks news", semi, "Mitsui Chemicals acquires JSR|10 Stocks Analysts Just Upgraded"},
		{"news cue never matches beside a topic", "semiconductor stocks news", []Result{{Title: "AP News: election results"}, {Title: "Breaking news: storm"}}, ""},
		{"et A2 set", "pooljuhtide aktsiad uudised", []Result{{Title: "Aasia investorid müüvad pooljuhtide aktsiaid"}, {Title: "Taiwan piirab lühikeseks müüki", Snippet: "kiibitootjate aktsiad"}}, "Aasia investorid müüvad pooljuhtide aktsiaid|Taiwan piirab lühikeseks müüki"},
		{"stem inside a compound", "euro kurss", []Result{{Title: "Eesti Panga vahetuskurss täna"}, {Title: "Ilm"}}, "Eesti Panga vahetuskurss täna"},
		{"short word starts a word only", "rimi", []Result{{Title: "Rimist leiab"}, {Title: "Riigi Teataja"}, {Title: "Prima"}}, "Rimist leiab"},
		{"no countable word keeps all", "ai", []Result{{Title: "x"}, {Title: "y"}}, "x|y"},
		{"et and English words together", "semikontaktorstide semiconductor", semi[:1], "Mitsui Chemicals acquires JSR"},
	}
	for _, c := range cases {
		var got []string
		for _, r := range Relevant(c.rs, c.q) {
			got = append(got, r.Title)
		}
		if g := strings.Join(got, "|"); g != c.want {
			t.Errorf("%s: Relevant(%q) = %q, want %q", c.name, c.q, g, c.want)
		}
	}
}
