package vars

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func loadIntentFile(t *testing.T, path string) []JsonIntent {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var in []JsonIntent
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func TestForkFilterKeepsExactlyTheAllowlist(t *testing.T) {
	files, _ := filepath.Glob("../../intent-data/*.json")
	if len(files) == 0 {
		t.Fatal("no intent-data files")
	}
	want := append([]string(nil), forkAllowedIntents...)
	sort.Strings(want)
	for _, f := range files {
		var got []string
		for _, it := range forkFilterIntents(loadIntentFile(t, f)) {
			got = append(got, it.Name)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: kept %v, want %v", filepath.Base(f), got, want)
		}
	}
}

func TestForkDroppedKeyphrasesExistInEnUS(t *testing.T) {
	src := map[string]map[string]bool{}
	for _, it := range loadIntentFile(t, "../../intent-data/en-US.json") {
		src[it.Name] = map[string]bool{}
		for _, k := range it.Keyphrases {
			src[it.Name][k] = true
		}
	}
	for name, kps := range forkDroppedKeyphrases {
		for _, k := range kps {
			if !src[name][k] {
				t.Errorf("%s: dropped keyphrase %q not in en-US.json", name, k)
			}
		}
	}
}

// forkMatch mirrors ProcessTextAll (ttr): exact match first, then ForkContainsPhrase unless requiresexact.
func forkMatch(intents []JsonIntent, text string) string {
	text = ForkNormalizeText(strings.ToLower(text))
	for _, it := range intents {
		for _, k := range it.Keyphrases {
			if text == strings.ToLower(k) {
				return it.Name
			}
		}
	}
	for _, it := range intents {
		for _, k := range it.Keyphrases {
			if ForkContainsPhrase(text, strings.ToLower(k)) && !it.RequireExactMatch {
				return it.Name
			}
		}
	}
	return ""
}

func TestForkFilterRoutesQuestionsToLLM(t *testing.T) {
	in := forkFilterIntents(loadIntentFile(t, "../../intent-data/en-US.json"))
	cases := map[string]string{
		"what should i cook tonight":           "",
		"how deep is the water":                "",
		"what are the names of the planets":    "",
		"whats on the other side of the moon":  "",
		"i wonder whether dogs dream":          "",
		"is it time for lunch":                 "",
		"capture the flag rules":               "",
		"lets have a conversation about space": "",
		"how does photosynthesis work":         "",
		"what does the name isaac mean":        "",
		"which way is clockwise":               "",
		"can i ask you a question about mars":  "",
		"whats the big picture":                "",
		"why am i tired all the time":          "",
		"is this photo real":                   "",
		"whats the weather":                    "intent_weather_extend",
		"take a picture":                       "intent_photo_take_extend",
		"can you take a photo of me":           "intent_photo_take_extend",
		"take a selfie":                        "intent_photo_take_extend",
		"what's the time":                      "intent_clock_time",
		"tell me the time":                     "intent_clock_time",
		"lets talk":                            "intent_knowledge_promptquestion",
		"my name is kristjan":                  "intent_names_username_extend",
		"what time is it":                      "intent_clock_time",
		"set a timer for five minutes":         "intent_clock_settimer_extend",
		"i have a question":                    "intent_knowledge_promptquestion",
		"check the timer":                      "intent_clock_checktimer",
	}
	for text, want := range cases {
		if got := forkMatch(in, text); got != want {
			t.Errorf("%q: matched %q, want %q", text, got, want)
		}
	}
}

// Stage 2.2 mic phrases as Whisper wrote them (raw, then lowercased by the ET router) plus collision guards.
func TestForkIntentsEstonian(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	in := forkFilterIntents(loadIntentFile(t, "../../intent-data/et-EE.json"))
	cases := map[string]string{
		"Mis kell on?":                   "intent_clock_time",
		"Paneme taimer viieks minutiks.": "intent_clock_settimer_extend",
		"Mis ilm on Tallinnas?":          "intent_weather_extend",
		"Kes ma olen?":                   "intent_names_ask",
		"Mis on Eesti pealinn?":          "",
		"Kui kaugel on kuu?":             "",
		"Räägi mulle üks nali.":          "",
		"Miks taevas on sinine?":         "",
		"Mitu päeva on aastas?":          "",
		"Kes kirjutas Tõde ja õigus?":    "",
		"Pane taimer viieks minutiks.":   "intent_clock_settimer_extend",
		"Kui palju taimeril aega on?":    "intent_clock_checktimer",
		"Mis mu nimi on?":                "intent_names_ask",
		"Minu nimi on Kristjan.":         "intent_names_username_extend",
		"Ütle kellaaega.":                "intent_clock_time",
		"Tee minust pilti.":              "intent_photo_take_extend",
		"Mul on küsimus.":                "intent_knowledge_promptquestion",
		"Kohv ilma suhkruta.":            "",
		"Mis on maailma kõrgeim mägi?":   "",
		"Mina olen väsinud.":             "",
		"Kus on kesklinn?":               "",
	}
	for text, want := range cases {
		if got := forkMatch(in, strings.ToLower(text)); got != want {
			t.Errorf("%q: matched %q, want %q", text, got, want)
		}
	}
}

func TestForkNormalizeTextEnglishUnchanged(t *testing.T) {
	lang.UseForTest(t, lang.English)
	if got := ForkNormalizeText("what time is it?"); got != "what time is it?" {
		t.Errorf("en-US: %q", got)
	}
	lang.UseForTest(t, lang.Estonian)
	if got := ForkNormalizeText("minu nimi on kristjan."); got != "minu nimi on kristjan" {
		t.Errorf("et-EE: %q", got)
	}
}

func TestForkContainsPhraseWholeWords(t *testing.T) {
	cases := []struct {
		text, phrase string
		want         bool
	}{
		{"how does photosynthesis work", "photo", false},
		{"what's up", "what", false},
		{"photo photosynthesis", "photosynthesis", true},
		{"photosynthesis photo", "photo", true},
		{"check the timer?", "timer", true},
		{"现在几点", "几点", true},
		{"anything", "", false},
		{"mitu päeva on", "eva", false},
		{"räägi mulle", "räägi", true},
		{"paneme taimerit", "taimer*", true},
		{"pane ataimer", "taimer*", false},
		{"ilma suhkruta", "ilm", false},
		{"anything", "*", false},
	}
	for _, c := range cases {
		if got := ForkContainsPhrase(c.text, c.phrase); got != c.want {
			t.Errorf("ForkContainsPhrase(%q, %q) = %v, want %v", c.text, c.phrase, got, c.want)
		}
	}
}
