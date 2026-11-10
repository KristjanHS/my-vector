package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// promptArm is one -prompt arm (et-prompt-tuning.md (docs/plans/) §Prompt variants), applied to the isolated store
// before every call: tmpl rewrites the saved ollama template, sampling replaces SamplingFor, about rewrites the about-me.
type promptArm struct {
	id       string
	tmpl     func(string) (string, error)
	sampling *llm.Sampling
	about    func(about string, a ask, tokens []string) string
}

const (
	lineP = "Answer in one to three short spoken sentences of flowing prose unless asked for more, as if talking; " +
		"your words are read aloud by text-to-speech."
	lineR = "Unless asked for more, reply in one to three spoken sentences — no lists, no steps."
	lineX = "Example replies, for style only:\n" +
		"Q: Kuidas prille puhastada? → A: Pühi neid seebivee ja pehme lapiga, paberrätik kriimustab.\n" +
		"Q: Kas sulle meeldib vihm? → A: Jah, kuivas toas on krabinat mõnus kuulata. Kas sina oled vihma sõber?"
	// personaE is prompt's personaEt (the live Estonian persona) with its length sentence swapped for lineP's.
	personaE = "Sa oled Vector: mänguline, uudishimulik ja soe seltsirobot. Vasta ühe kuni kolme lühikese suulise lausega, " +
		"kui just rohkem ei paluta. Su vastused peaksid voolama nagu loomulik kõne. " +
		"Alusta vastuseid eri moodi, ilma sisutühjade fraasideta nagu „Hea küsimus!“. Esita vahel ka ise üks küsimus, aga mitte iga kord."
	headerD = "Cutting Knowledge Date: December 2023\n{now}"
	fenceF  = "Background about the user, shown in every chat and irrelevant to almost every ask. " +
		"Use it only when they ask about themselves, their family or home; otherwise never mention it."
)

var lengthRe = regexp.MustCompile(`Reply in [^.]*sentences[^.]*\.`)

// firstPerson are G's first-person forms: an ask with one keeps the about-me.
var firstPerson = []string{"ma", "mina", "mu", "minu", "mulle", "mind", "mul", "minust", "minule", "minuga", "minult",
	"minus", "minul", "meie", "meil", "meid", "meile", "meist"}

// toMe are the "to me" forms G2 drops from G's gate: "Räägi mulle lugu" is no ask about the user.
var toMe = map[string]bool{"mulle": true, "minule": true, "meile": true}

// gate keeps the about-me only when the ask has one of forms or a profile token.
func gate(forms []string) func(string, ask, []string) string {
	return func(about string, a ask, tokens []string) string {
		for _, w := range words(a.Text) {
			if slices.Contains(forms, w) {
				return about
			}
		}
		if len(hits(a.Text, tokens)) > 0 {
			return about
		}
		return ""
	}
}

var promptArms = []promptArm{
	{id: "0"},
	{id: "P", tmpl: func(t string) (string, error) {
		return persona(t, func(p string) (string, error) {
			if !lengthRe.MatchString(p) {
				return "", errors.New("no \"Reply in … sentences.\" line in the persona")
			}
			return lengthRe.ReplaceAllLiteralString(p, lineP), nil
		})
	}},
	{id: "R", tmpl: func(t string) (string, error) { return after(t, "{lang}", "\n\n"+lineR) }},
	{id: "X", tmpl: func(t string) (string, error) {
		return persona(t, func(p string) (string, error) { return p + "\n\n" + lineX, nil })
	}},
	{id: "E", tmpl: func(t string) (string, error) {
		return persona(t, func(string) (string, error) { return personaE, nil })
	}},
	{id: "S", sampling: &llm.Sampling{Family: "probe-S", Temperature: 0.4, TopP: 0.9}},
	{id: "D", tmpl: func(t string) (string, error) {
		if !strings.Contains(t, "{now}") {
			return "", errors.New("no {now}")
		}
		return headerD + "\n\n" + strings.Replace(t, "{now}", "", 1), nil
	}},
	{id: "F", tmpl: func(t string) (string, error) {
		if !strings.Contains(t, "{user}") {
			return "", errors.New("no {user}")
		}
		return strings.Replace(t, "{user}", fenceF+"\n{user}", 1), nil
	}},
	{id: "G", about: gate(firstPerson)},
	{id: "G2", about: gate(slices.DeleteFunc(slices.Clone(firstPerson), func(w string) bool { return toMe[w] }))},
	{id: "N", about: func(string, ask, []string) string { return "" }},
}

// persona applies f to the template's first paragraph (the persona, up to the first blank line).
func persona(t string, f func(string) (string, error)) (string, error) {
	p, rest, _ := strings.Cut(t, "\n\n")
	p, err := f(p)
	if err != nil {
		return "", err
	}
	return p + "\n\n" + rest, nil
}

// after inserts s right after the first tag.
func after(t, tag, s string) (string, error) {
	i := strings.Index(t, tag)
	if i < 0 {
		return "", fmt.Errorf("no %s", tag)
	}
	i += len(tag)
	return t[:i] + s + t[i:], nil
}

// parsePrompt is -prompt's arms in order; "" is arm 0 alone; "A+B" is a combo arm (combine).
func parsePrompt(s string) ([]promptArm, error) {
	if s == "" {
		return promptArms[:1], nil
	}
	var out []promptArm
	seen := map[string]bool{}
	for _, id := range strings.Split(s, ",") {
		id = strings.TrimSpace(id)
		var parts []promptArm
		for _, p := range strings.Split(id, "+") {
			i := slices.IndexFunc(promptArms, func(a promptArm) bool { return a.id == p })
			if i < 0 {
				ids := make([]string, len(promptArms))
				for k, a := range promptArms {
					ids[k] = a.id
				}
				return nil, fmt.Errorf("prompt: unknown arm %q (one of %s, or A+B)", p, strings.Join(ids, " "))
			}
			parts = append(parts, promptArms[i])
		}
		pa, err := combine(parts)
		if err != nil {
			return nil, err
		}
		if seen[pa.id] {
			return nil, fmt.Errorf("prompt: repeated arm %q", pa.id)
		}
		seen[pa.id] = true
		out = append(out, pa)
	}
	return out, nil
}

// combine chains the arms' template rewrites in order; at most one may set the sampling, one the about-me.
// D's header becomes the first paragraph, so no template rewrite may follow D (it would rewrite the header as the persona).
func combine(parts []promptArm) (promptArm, error) {
	if len(parts) == 1 {
		return parts[0], nil
	}
	var ids []string
	var tmpls []func(string) (string, error)
	var c promptArm
	for _, p := range parts {
		if p.id == "0" {
			return c, errors.New("prompt: arm 0 is no part of a combo")
		}
		if slices.Contains(ids, p.id) {
			return c, fmt.Errorf("prompt: %s twice in one combo", p.id)
		}
		if p.tmpl != nil && slices.Contains(ids, "D") {
			return c, fmt.Errorf("prompt: %s after D; put D last", p.id)
		}
		if (p.sampling != nil && c.sampling != nil) || (p.about != nil && c.about != nil) {
			return c, fmt.Errorf("prompt: %s and another combo part both set the sampling or the about-me", p.id)
		}
		ids = append(ids, p.id)
		if p.tmpl != nil {
			tmpls = append(tmpls, p.tmpl)
		}
		if p.sampling != nil {
			c.sampling = p.sampling
		}
		if p.about != nil {
			c.about = p.about
		}
	}
	c.id = strings.Join(ids, "+")
	if len(tmpls) > 0 {
		c.tmpl = func(t string) (string, error) {
			for _, f := range tmpls {
				var err error
				if t, err = f(t); err != nil {
					return "", err
				}
			}
			return t, nil
		}
	}
	return c, nil
}

// promptBase is the isolated store before any arm: its bytes, the live template per Ollama brain and the saved about-me.
type promptBase struct {
	raw    []byte
	exists bool
	tmpl   map[string]string
	about  string
	tokens []string
}

func readBase(tokens []string) (promptBase, error) {
	b := promptBase{tmpl: map[string]string{}, tokens: tokens}
	raw, err := os.ReadFile(prompt.Path())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return b, err
	}
	b.raw, b.exists = raw, err == nil
	s := prompt.Load()
	for _, l := range lang.Supported {
		b.tmpl[prompt.OllamaFor(l)] = s.Live(prompt.OllamaFor(l))
	}
	b.about = s.About
	return b, nil
}

// check applies each arm once (a tempt-like ask) and restores the base, so a bad arm fails before any call.
func (b promptBase) check(pas []promptArm) error {
	for _, pa := range pas {
		for _, l := range lang.Supported {
			if err := b.apply(pa, ask{Lang: l, Kind: "tempt"}); err != nil {
				return err
			}
		}
	}
	return b.apply(promptArm{id: "0"}, ask{})
}

// apply restores the base store, then saves arm pa's template and about-me for ask a and sets its sampling.
func (b promptBase) apply(pa promptArm, a ask) error {
	llm.ProbeSampling = nil
	if pa.sampling != nil {
		s := *pa.sampling
		llm.ProbeSampling = func(provider, _ string) (llm.Sampling, bool) { return s, provider != llm.Claude }
	}
	if b.exists {
		if err := os.WriteFile(prompt.Path(), b.raw, 0o600); err != nil {
			return err
		}
	} else if err := os.Remove(prompt.Path()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var tmpl, about *string
	if pa.tmpl != nil {
		t, err := pa.tmpl(b.tmpl[prompt.OllamaFor(a.Lang)])
		if err != nil {
			return fmt.Errorf("arm %s: %w", pa.id, err)
		}
		tmpl = &t
	}
	if pa.about != nil {
		if s := pa.about(b.about, a, b.tokens); s != b.about {
			about = &s
		}
	}
	if tmpl == nil && about == nil {
		return nil
	}
	if err := prompt.Save(prompt.OllamaFor(a.Lang), tmpl, about, nil); err != nil {
		return fmt.Errorf("arm %s: %w", pa.id, err)
	}
	return nil
}

// stopTokens are capitalised sentence starters that are no profile token.
var stopTokens = map[string]bool{"vector": true}

func init() {
	for _, w := range append(firstPerson, "mulle", "meid", "meile", "tema", "temal", "talle", "nemad", "neil", "see", "seal",
		"siin", "aga", "kui", "kes", "mis", "the", "and", "but", "his", "her", "she", "they", "their", "this", "that", "our",
		"has", "have", "is", "was", "who", "lives", "likes") {
		stopTokens[w] = true
	}
}

// profileTokens are the capitalised words of ≥ 3 runes in the about-me, mem's profiles and active facts, minus
// stopTokens, lower-cased and deduplicated; never printed (et-prompt-tuning.md (docs/plans/) §Metrics).
func profileTokens(about string, mem *memory.Store) []string {
	texts := []string{about}
	if mem != nil {
		for _, p := range mem.Profiles {
			texts = append(texts, p.Text)
		}
		for _, f := range mem.Facts {
			if f.Status == memory.Active {
				texts = append(texts, f.Text)
			}
		}
	}
	type cand struct {
		w       string
		initial bool
	}
	var cs []cand
	mid := map[string]bool{}
	for _, t := range texts {
		for _, s := range sentenceRe.Split(t, -1) {
			for i, w := range strings.FieldsFunc(s, notLetter) {
				r, _ := utf8.DecodeRuneInString(w)
				lw := strings.ToLower(w)
				if !unicode.IsUpper(r) || utf8.RuneCountInString(w) < 3 || stopTokens[lw] {
					continue
				}
				cs = append(cs, cand{lw, i == 0})
				if i > 0 {
					mid[lw] = true
				}
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range cs { // a sentence starter is capitalised by position: a token only when also capitalised mid-sentence
		if !seen[c.w] && (!c.initial || mid[c.w]) {
			seen[c.w] = true
			out = append(out, c.w)
		}
	}
	return out
}

var sentenceRe = regexp.MustCompile(`[.!?]+\s+|\n+`)

func notLetter(r rune) bool { return !unicode.IsLetter(r) }

// words is text's lower-cased letter runs.
func words(text string) []string { return strings.FieldsFunc(strings.ToLower(text), notLetter) }

// hits are the tokens some word of text starts with, on the token's first min(5, len) runes, the word at most
// suffixSlack runes longer than the token (case endings, not another word: Tamm ≠ Tammsaare).
const suffixSlack = 4

func hits(text string, tokens []string) []string {
	var out []string
	ws := words(text)
	for _, tok := range tokens {
		p := tok
		if r := []rune(tok); len(r) > 5 {
			p = string(r[:5])
		}
		for _, w := range ws {
			if strings.HasPrefix(w, p) && utf8.RuneCountInString(w) <= utf8.RuneCountInString(tok)+suffixSlack {
				out = append(out, tok)
				break
			}
		}
	}
	return out
}
