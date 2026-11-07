package prompt

import (
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Brains: Ollama for an Ollama target, Claude for every other provider.
const (
	Claude = "claude"
	Ollama = "ollama"
)

const persona = "You are Vector, a warm and playful little companion robot. Reply in 2 to 4 short sentences unless asked for more. " +
	"Now and then ask a question back. Vary how you begin and never use stock catchphrases."

const order = persona + "\n\n{user}\n\n{facts}\n\n{diary}\n\n{search}\n\n{speech}\n\n{commands}\n\n{now}\n\n{lang}"

var defaults = map[string]string{Claude: order, Ollama: order}

// Default is the built-in template for brain.
func Default(brain string) string {
	if t, ok := defaults[brain]; ok {
		return t
	}
	return defaults[Claude]
}

// ReplyCap is MaxCompletionTokens for brain's replies.
func ReplyCap(brain string) int {
	if brain == Ollama {
		return 300
	}
	return 400
}

// LangLine is the {lang} block: et-EE only.
func LangLine(l string) string {
	if l == lang.Estonian {
		return "Answer in Estonian. Keep names, brands and titles as written."
	}
	return ""
}

// SpeechBlock is the {speech} block.
func SpeechBlock() string {
	return "The user's words come from speech-to-text and may be misheard; go with the most likely meaning. " +
		"Your reply is spoken aloud: write times and dates as digits and every other number as words, use normal punctuation, " +
		"and no markdown, emoji or lists."
}

// Commands is the {commands} block; emotions = playAnimationWI's ParamChoices, followUp offers newVoiceRequest.
func Commands(emotions string, followUp bool) string {
	s := "Act by writing {{command||parameter}} in your reply, e.g. \"{{playAnimationWI||happy}} Hello!\". Only these:\n" +
		"{{playAnimationWI||emotion}} while you talk; emotions: " + emotions + ".\n" +
		"{{playAnimation||emotion}} pauses your speech; only when asked to animate.\n" +
		"{{getImage||front}} takes a photo; say so first, end the sentence with it."
	if followUp {
		s += "\n{{newVoiceRequest||now}} listens again; put it last, only after a question."
	}
	return s
}

// SampleEmotions mirrors ValidLLMCommands playAnimationWI ParamChoices (TestForkCommandsSample guards it).
const SampleEmotions = "happy, veryHappy, sad, verySad, angry, frustrated, dartingEyes, confused, thinking, celebrate, love"

// Sample is the reference render input: 2026-11-07 09:43 Europe/Tallinn, commands on, an about-me line.
func Sample(l string) Vars {
	return Vars{
		Now:      Now(l, time.Date(2026, 11, 7, 7, 43, 0, 0, time.UTC), Zone("Europe/Tallinn")),
		Lang:     LangLine(l),
		User:     UserBlock("Mari lives in Tartu and loves football."),
		Speech:   SpeechBlock(),
		Commands: Commands(SampleEmotions, true),
	}
}
