package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

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
		"Q: Kui kaugel on Päike? → A: Päike on Maast umbes sada viiskümmend miljonit kilomeetrit eemal. Selle valgus jõuab meieni umbes kaheksa minutiga.\n" +
		"Q: Kuidas prille puhastada? → A: Pihusta neile veidi seebivett ja pühi pehme mikrokiudlapiga. Paberrätik võib klaasi kriimustada.\n" +
		"Q: Kas sulle meeldib vihm? → A: Vihm on mõnus, kui saan kuivas toas selle krabinat kuulata. Kas sina oled pigem päikese või vihma sõber?"
	// personaE is the saved persona with lineP, in Estonian.
	personaE = "Sa oled Vector, mänguline ja lõbus, aga mitte tüütu kaaslasrobot. Vasta ühe kuni kolme lühikese suulise lausega " +
		"voolava jutuna, kui just rohkem ei paluta, nagu räägiksid; sinu sõnad loetakse ette kõnesünteesiga. " +
		"Aeg-ajalt küsi ise vastu. Alusta vastuseid eri moodi ja ära kasuta kulunud fraase."
	headerD = "Cutting Knowledge Date: December 2023\n{now}"
	fenceF  = "Background about the user, shown in every chat and irrelevant to almost every ask. " +
		"Use it only when they ask about themselves, their family or home; otherwise never mention it."
)

var lengthRe = regexp.MustCompile(`Reply in [^.]*sentences[^.]*\.`)

// firstPerson are G's first-person forms: an ask with one keeps the about-me.
var firstPerson = []string{"ma", "mina", "mu", "minu", "mulle", "mind", "mul", "meie", "meil"}

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
	{id: "G", about: func(about string, a ask, tokens []string) string {
		for _, w := range words(a.Text) {
			for _, f := range firstPerson {
				if w == f {
					return about
				}
			}
		}
		if len(hits(a.Text, tokens)) > 0 {
			return about
		}
		return ""
	}},
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

// parsePrompt is -prompt's arms in order; "" is arm 0 alone.
func parsePrompt(s string) ([]promptArm, error) {
	if s == "" {
		return promptArms[:1], nil
	}
	var out []promptArm
	seen := map[string]bool{}
	for _, id := range strings.Split(s, ",") {
		id = strings.TrimSpace(id)
		i := -1
		for k, a := range promptArms {
			if a.id == id {
				i = k
			}
		}
		if i < 0 || seen[id] {
			return nil, fmt.Errorf("prompt: unknown or repeated arm %q (one of 0 P R X E S D F G N)", id)
		}
		seen[id] = true
		out = append(out, promptArms[i])
	}
	return out, nil
}

// promptBase is the isolated store before any arm: its bytes, the live ollama template and the saved about-me.
type promptBase struct {
	raw    []byte
	exists bool
	tmpl   string
	about  string
	tokens []string
}

func readBase(tokens []string) (promptBase, error) {
	b := promptBase{tokens: tokens}
	raw, err := os.ReadFile(prompt.Path())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return b, err
	}
	b.raw, b.exists = raw, err == nil
	s := prompt.Load()
	b.tmpl, b.about = s.Live(prompt.Ollama), s.About
	return b, nil
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
		t, err := pa.tmpl(b.tmpl)
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
	if err := prompt.Save(prompt.Ollama, tmpl, about); err != nil {
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
	var out []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, w := range strings.FieldsFunc(t, notLetter) {
			r, _ := utf8.DecodeRuneInString(w)
			lw := strings.ToLower(w)
			if !unicode.IsUpper(r) || utf8.RuneCountInString(w) < 3 || stopTokens[lw] || seen[lw] {
				continue
			}
			seen[lw] = true
			out = append(out, lw)
		}
	}
	return out
}

func notLetter(r rune) bool { return !unicode.IsLetter(r) }

// words is text's lower-cased letter runs.
func words(text string) []string { return strings.FieldsFunc(strings.ToLower(text), notLetter) }

// hits are the tokens some word of text starts with, on the token's first min(5, len) runes.
func hits(text string, tokens []string) []string {
	var out []string
	ws := words(text)
	for _, tok := range tokens {
		p := tok
		if r := []rune(tok); len(r) > 5 {
			p = string(r[:5])
		}
		for _, w := range ws {
			if strings.HasPrefix(w, p) {
				out = append(out, tok)
				break
			}
		}
	}
	return out
}
