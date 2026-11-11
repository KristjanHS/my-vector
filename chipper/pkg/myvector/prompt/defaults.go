package prompt

import (
	"slices"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Brains: Ollama (en) / OllamaEt (et-EE) for an Ollama target, Claude for every other provider.
const (
	Claude   = "claude"
	Ollama   = "ollama"
	OllamaEt = "ollama-et"
)

// Brains lists every brain, in page tab order.
var Brains = []string{Claude, Ollama, OllamaEt}

// Known reports whether brain is one of Brains.
func Known(brain string) bool { return slices.Contains(Brains, brain) }

// IsOllama reports an Ollama brain (either language).
func IsOllama(brain string) bool { return brain == Ollama || brain == OllamaEt }

// OllamaFor is the Ollama brain for reply language l.
func OllamaFor(l string) string {
	if l == lang.Estonian {
		return OllamaEt
	}
	return Ollama
}

const (
	singAnchor = "Now and then ask a question back."
	// persona and personaEt are the user's saved live templates (2026-11-10); neither has SingEt.
	persona = "You are Vector: a playful, curious and warm companion robot. Reply in 1-2 short sentences, unless you are asked for more. " +
		"Your replies should flow like natural speech. Vary how you begin: no filler openers like 'Great question!'. " +
		"Ask at most one question back, and not every time."
	personaEt = "Sa oled Vector: mänguline, uudishimulik ja soe seltsirobot. Vasta 1-2 lühikese lausega, kui just rohkem ei paluta. " +
		"Su vastused peaksid voolama nagu loomulik kõne. Alusta vastuseid eri moodi, ilma sisutühjade fraasideta nagu „Hea küsimus!“. " +
		"Esita vahel ka ise üks küsimus, aga mitte iga kord."
)

// SingEt is the Estonian sing line (plan ollama-et-prompt R5); no built-in carries it, only Store.seed's WithSing adds it.
const SingEt = "Sa räägid valjult oma robotikehaga: kui palutakse, laulad laulu, öeldes selle sõnad."

// WithSing puts SingEt right after singAnchor; a template without the anchor, or already with SingEt, is unchanged.
func WithSing(t string) string {
	if strings.Contains(t, SingEt) {
		return t
	}
	return strings.Replace(t, singAnchor, singAnchor+" "+SingEt, 1)
}

const tags = "\n\n{user}\n\n{facts}\n\n{diary}\n\n{search}\n\n{speech}\n\n{commands}\n\n{now}\n\n{lang}"

var defaults = map[string]string{Claude: persona + tags, Ollama: persona + tags, OllamaEt: personaEt + tags}

// Default is the built-in template for brain.
func Default(brain string) string {
	if t, ok := defaults[brain]; ok {
		return t
	}
	return defaults[Claude]
}

// ReplyCap caps brain's reply tokens: sent as max_tokens to Ollama, else max_completion_tokens (ttr forkReplyCap).
func ReplyCap(brain string) int {
	if IsOllama(brain) {
		return 300
	}
	return 1024 // Claude thinks inside the cap (plan ruling RC)
}

// LongReplyCap replaces ReplyCap for a long ask: a story, detail, N sentences, or the KG path (ttr forkAIReq).
func LongReplyCap(brain string) int {
	if IsOllama(brain) {
		return 1000
	}
	return 2048
}

// OllamaEt's {speech}, {commands} and {lang} blocks are Estonian, like its persona: the et model needs Estonian
// instructions (plan ollama-et-prompt P14).

// LangLine is brain's {lang} block: et-EE only.
func LangLine(brain, l string) string {
	switch {
	case l != lang.Estonian:
		return ""
	case brain == OllamaEt:
		return "Vasta eesti keeles. Ära tõlgi nimesid, kaubamärke ega pealkirju." // "as written" could read as "don't inflect"
	}
	return "Answer in Estonian. Keep names, brands and titles as written."
}

// SpeechBlock is brain's {speech} block.
func SpeechBlock(brain string) string {
	if brain == OllamaEt {
		return "Kasutaja sõnad tulevad kõnetuvastusest ja võivad olla valesti kuuldud; lähtu kõige tõenäolisemast tähendusest. " +
			"Sinu vastus loetakse valjult ette: kirjuta kellaajad ja kuupäevad numbritega, kõik muud arvud sõnadega " +
			"ja kasuta tavalisi kirjavahemärke, aga mitte markdowni, emotikone ega loendeid."
	}
	return "The user's words come from speech-to-text and may be misheard; go with the most likely meaning. " +
		"Your reply is spoken aloud: write times and dates as digits and every other number as words, use normal punctuation, " +
		"and no markdown, emoji or lists."
}

// SearchWrap heads brain's filled {search} block; the Ollama brains also get the usage rule (search-news-quality N5).
func SearchWrap(brain string) string {
	switch brain {
	case OllamaEt:
		return "Värsked veebitulemused (kuupäev, allikas): vasta nende põhjal 2–3 lausega, nimeta allikas ja kuupäev. " +
			"Kui need ei vasta küsimusele, ütle, et ei leidnud sobivat vastust. Ära ütle, et sa ei saa otsida."
	case Ollama:
		return "Fresh web results (date, source): answer from them in 2–3 sentences, naming the source and date. " +
			"If they don't answer the question, say you found nothing fitting. Never say you can't search."
	}
	return "Live web results for the user's question:"
}

// NoSearch fills an Ollama brain's empty {search} block (search-trigger-fixes F4); Claude's stays empty. No "don't
// repeat your previous reply" clause: replay copied turn 1 verbatim 2/3 with it, 1/3 without (R5).
func NoSearch(brain string) string {
	switch brain {
	case OllamaEt:
		return "Sul pole praegu otsingutulemusi ja sa ei saa ise internetist otsida. Kui küsitakse värskeid andmeid (uudised, hinnad, " +
			"tulemused), ütle ühe lausega, et sa ei saanud seda järele vaadata; ära paku otsimist ega loetle."
	case Ollama:
		return "You have no web results now and can't browse. If asked for live facts (news, prices, results), say in one sentence " +
			"that you couldn't look it up; no offer to search and no list."
	}
	return ""
}

// Commands is brain's {commands} block; emotions = playAnimationWI's ParamChoices. Command names and emotions stay
// English in every language: the reply parser matches them. No newVoiceRequest: forkFollowUp listens again (F5).
func Commands(brain, emotions string) string {
	if brain == OllamaEt {
		return animations(brain, emotions) + "\n{{getImage||front}} teeb foto; ütle enne, et teed foto, ja pane käsk lause lõppu."
	}
	return animations(brain, emotions) + "\n{{getImage||front}} takes a photo; say so first, end the sentence with it."
}

// PhotoCommands is Commands for a photo answer: no getImage, PhotoLine after the animations.
func PhotoCommands(brain, emotions string) string {
	return animations(brain, emotions) + "\n\n" + PhotoLine(brain)
}

// PhotoLine tells a photo answer the image is its own, already-taken photo.
func PhotoLine(brain string) string {
	if brain == OllamaEt {
		return "Sa näed: viimases sõnumis olev pilt on see, mida su enda kaamera praegu näeb. Ütle, mida sa sellel näed; ära ütle, et sa ei näe."
	}
	return "You can see: the image in the last message is what your own camera sees right now. Say what you see in it; never say you can't see."
}

func animations(brain, emotions string) string {
	if brain == OllamaEt {
		return "Tegutse, kirjutades vastusesse {{käsk||parameeter}}, nt \"{{playAnimationWI||happy}} Tere!\". Ainult need:\n" +
			"{{playAnimationWI||emotsioon}} rääkimise ajal; emotsioonid (kirjuta täpselt nii): " + emotions + ".\n" +
			"{{playAnimation||emotsioon}} peatab su kõne; ainult siis, kui palutakse animatsiooni teha."
	}
	return "Act by writing {{command||parameter}} in your reply, e.g. \"{{playAnimationWI||happy}} Hello!\". Only these:\n" +
		"{{playAnimationWI||emotion}} while you talk; emotions: " + emotions + ".\n" +
		"{{playAnimation||emotion}} pauses your speech; only when asked to animate."
}

// SampleEmotions mirrors ValidLLMCommands playAnimationWI ParamChoices (TestForkCommandsSample guards it).
const SampleEmotions = "happy, veryHappy, sad, verySad, angry, frustrated, dartingEyes, confused, thinking, celebrate, love"

// Sample is brain's reference render input: 2026-11-07 09:43 Europe/Tallinn, commands on, an about-me line
// (OllamaEt's in Estonian, as userFor picks it).
func Sample(brain, l string) Vars {
	return Vars{
		Now:      Now(l, time.Date(2026, 11, 7, 7, 43, 0, 0, time.UTC), Zone("Europe/Tallinn")),
		Lang:     LangLine(brain, l),
		User:     userFor(brain, "Mari lives in Tartu and loves football.", "Mari elab Tartus ja armastab jalgpalli."),
		Speech:   SpeechBlock(brain),
		Commands: Commands(brain, SampleEmotions),
	}
}
