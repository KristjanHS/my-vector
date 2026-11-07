package vars

import "strings"

// Asks that want a long reply (2026-11-07-reply-cap-by-ask.md, docs/plans/ ruling D1); en + et in one list.
var forkLongAskPhrases = []string{
	"story", "stories", "fairy tale", "in detail", "detailed", "step by step", "sentences", "paragraphs",
	"lugu", "lugusid", "jutusta*", "muinasjut*", "pikemalt", "üksikasjalik*", "põhjalikult", "samm-sammult", "lauset", "lõiku",
}

// ForkLongAsk reports whether text hits a long-ask phrase (whole words, ForkContainsPhrase).
func ForkLongAsk(text string) bool {
	text = strings.ToLower(text)
	for _, p := range forkLongAskPhrases {
		if ForkContainsPhrase(text, p) {
			return true
		}
	}
	return false
}
