package vars

import (
	"strings"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Built-in intents that still match before the LLM (2026-11-05-intent-allowlist.md, docs/plans/).
var forkAllowedIntents = []string{
	"intent_clock_time",
	"intent_clock_settimer_extend",
	"intent_clock_checktimer",
	"intent_weather_extend",
	"intent_photo_take_extend",
	"intent_names_username_extend",
	"intent_names_ask",
	"intent_knowledge_promptquestion",
}

// Keyphrases that hijack ordinary questions via substring match ("how deep is the water" → weather).
var forkDroppedKeyphrases = map[string][]string{
	"intent_weather_extend":           {"whether", "the other", "the water", "no other"},
	"intent_names_username_extend":    {"names"},
	"intent_photo_take_extend":        {"capture", "photo", "foto", "picture", "selby"},
	"intent_knowledge_promptquestion": {"conversation", "question", "weston"},
	"intent_clock_settimer_extend":    {"time for", "time or", "time of", "time of for"},
	"intent_clock_time":               {"the time"},
}

// Command phrases that replace the dropped bare nouns.
var forkAddedKeyphrases = map[string][]string{
	"intent_photo_take_extend": {"take a photo", "take a picture", "take a selfie", "take my picture", "take a pic"},
	"intent_clock_time":        {"what's the time", "whats the time", "tell me the time"},
}

// forkIsWord: ASCII alnum, ', and Latin letters U+00C0–U+024F (ä õ ö ü š ž); CJK etc. stay separators so zh-CN still matches.
func forkIsWord(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '\'' || r >= 0xC0 && r <= 0x24F && r != 0xD7 && r != 0xF7
}

// ForkContainsPhrase is ProcessTextAll's partial match on whole words; a trailing "*" (E22) matches any word ending.
func ForkContainsPhrase(text, phrase string) bool {
	stem := strings.HasSuffix(phrase, "*")
	phrase = strings.TrimSuffix(phrase, "*")
	if phrase == "" {
		return false
	}
	for i := 0; i < len(text); {
		j := strings.Index(text[i:], phrase)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(phrase)
		before, _ := utf8.DecodeLastRuneInString(text[:j])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if (j == 0 || !forkIsWord(before)) && (stem || end == len(text) || !forkIsWord(after)) {
			return true
		}
		i = j + 1
	}
	return false
}

// ForkNormalizeText trims Whisper's end punctuation in Estonian mode so names and exact matches stay clean.
func ForkNormalizeText(text string) string {
	if ForkLanguage() != lang.Estonian {
		return text
	}
	return strings.TrimRight(text, " .?!…")
}

// forkFilterIntents keeps only allowlisted intents, in file order, minus dropped and plus added keyphrases.
func forkFilterIntents(in []JsonIntent) []JsonIntent {
	allowed := map[string]bool{}
	for _, n := range forkAllowedIntents {
		allowed[n] = true
	}
	var out []JsonIntent
	for _, it := range in {
		if !allowed[it.Name] {
			continue
		}
		drop := map[string]bool{}
		for _, k := range forkDroppedKeyphrases[it.Name] {
			drop[k] = true
		}
		var kps []string
		for _, k := range it.Keyphrases {
			if !drop[k] {
				kps = append(kps, k)
			}
		}
		it.Keyphrases = append(kps, forkAddedKeyphrases[it.Name]...)
		out = append(out, it)
	}
	return out
}
