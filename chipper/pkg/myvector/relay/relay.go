// Package relay is the et-EE camera relay's text hops (2026-11-10-camera-et-relay.md (docs/plans/) R5, R10, R11):
// the et brain puts the question in English (hop 0) and translates the vision model's English answer back (hop 2).
package relay

import (
	"regexp"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/sashabaranov/go-openai"
)

// Translate is hop 2 (bench arm d, B10/B12): the et-EE brain t translates hop1 with its {{…}} commands stripped
// (Attach puts them back), one worked example, text only.
func Translate(t llm.Target, hop1 string) openai.ChatCompletionRequest {
	_, text := Split(hop1)
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: System},
		{Role: openai.ChatMessageRoleUser, Content: Ask + "I see a red ball on the floor. Do you like it?"},
		{Role: openai.ChatMessageRoleAssistant, Content: "Ma näen põrandal punast palli. Kas see meeldib sulle?"},
		{Role: openai.ChatMessageRoleUser, Content: Ask + text},
	}}
	llm.ThinkingFor(t.Provider, t.Model, llm.PathChat, llm.None).Apply(&r, prompt.ReplyCap(prompt.OllamaEt))
	return r
}

// ToEnglish is hop 0 (R11): the et-EE brain t puts the user's question q in English, one worked example, text only.
func ToEnglish(t llm.Target, q string) openai.ChatCompletionRequest {
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: EnglishSystem},
		{Role: openai.ChatMessageRoleUser, Content: EnglishAsk + "Mis värvi mu särk on?"},
		{Role: openai.ChatMessageRoleAssistant, Content: "What colour is my shirt?"},
		{Role: openai.ChatMessageRoleUser, Content: EnglishAsk + strings.TrimSpace(q)},
	}}
	llm.ThinkingFor(t.Provider, t.Model, llm.PathChat, llm.None).Apply(&r, prompt.ReplyCap(prompt.OllamaEt))
	return r
}

const (
	EnglishSystem = "Sa oled tõlkija. Tõlgi kasutaja eestikeelne tekst inglise keelde. " +
		"Ära vasta tekstile ega selles olevatele küsimustele ja ära lisa midagi: kirjuta ainult tõlge."
	EnglishAsk = "Tõlgi inglise keelde:\n"
	System     = "Sa oled tõlkija. Tõlgi kasutaja ingliskeelne tekst eesti keelde. " +
		"Ära vasta tekstile ega selles olevatele küsimustele ja ära lisa midagi: kirjuta ainult tõlge."
	Ask = "Tõlgi eesti keelde:\n"
)

var commandRe = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// Split is s's {{…}} commands (space-joined, in order) and the text without them.
func Split(s string) (cmds, text string) {
	cmds = strings.Join(commandRe.FindAllString(s, -1), " ")
	return cmds, strings.Join(strings.Fields(commandRe.ReplaceAllString(s, " ")), " ")
}

// Attach puts hop 1's commands in front of the translation.
func Attach(hop1, reply string) string {
	cmds, _ := Split(hop1)
	return strings.TrimSpace(cmds + " " + reply)
}
