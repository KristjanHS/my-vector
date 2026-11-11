package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Path is the saved prompt file; vars/fork_prompt.go points it beside apiConfig.json (prompt must not import vars).
var Path = func() string { return "myvectorPrompt.json" }

// AboutCap is the limit in runes (≈150 tokens) of each about-me (About, AboutEt).
const AboutCap = 450

const keepVersions = 5

// RequiredTags are appended, in this (the default template's) order, to a template that lacks them (Complete);
// the other KnownTags are optional.
var (
	RequiredTags = []string{"{speech}", "{commands}", "{now}", "{lang}"}
	KnownTags    = []string{"{now}", "{lang}", "{user}", "{facts}", "{diary}", "{search}", "{speech}", "{commands}"}
)

// Budget is the token ceiling for brain's template rendered with Sample, empty facts/diary/search and the about-me.
// OllamaEt gets more: Estonian text runs longer than the same English (user ruling 2026-11-10).
func Budget(brain string) int {
	switch brain {
	case OllamaEt:
		return 700
	case Ollama:
		return 600
	}
	return 2000
}

// SearchBudget is the token ceiling for brain's {search} block (plan web-search W9).
func SearchBudget(brain string) int {
	if IsOllama(brain) {
		return 340 // the N5 usage rule counts inside the block (search-news-quality Q5)
	}
	return 800
}

// MemoryBudget is the token ceiling for brain's {facts} + {diary} blocks together (memory.Block fits it).
func MemoryBudget(brain string) int {
	if IsOllama(brain) {
		return 300
	}
	return 3000
}

var logf = logger.Println

// Invalid marks a rejected brain, template, about-me or version (the page's 400); any other store error is I/O.
type Invalid struct{ error }

// Store is myvectorPrompt.json: per-brain saved template and versions (newest first), and the about-me texts.
type Store struct {
	Templates map[string]string   `json:"templates,omitempty"`
	Versions  map[string][]string `json:"versions,omitempty"`
	About     string              `json:"about,omitempty"`
	AboutEt   string              `json:"about_et,omitempty"` // OllamaEt's only (plan ollama-et-prompt R8/R9)
}

// Load reads the store for rendering; an unreadable or unparsable file is empty and logged.
func Load() Store {
	s, err := load()
	if err != nil {
		logf("myvector prompt: " + err.Error() + "; using the built-in templates")
	}
	return s
}

// load reads the store; a missing file is empty, an unreadable or unparsable one an error (I/O, not Invalid),
// so a write never replaces a file it could not read.
func load() (Store, error) {
	var s Store
	b, err := os.ReadFile(Path())
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		return Store{}, fmt.Errorf("%s: %w; fix or delete it", Path(), err)
	}
	s.seed()
	return s, nil
}

// seed gives a store with no OllamaEt template or versions that brain from its saved Ollama template (WithSing) and versions;
// the next templates write persists it. A saved OllamaEt version (Reset keeps them) stops the re-seed.
func (s *Store) seed() {
	_, et := s.Templates[OllamaEt]
	t, en := s.Templates[Ollama]
	if et || len(s.Versions[OllamaEt]) > 0 || (!en && len(s.Versions[Ollama]) == 0) {
		return
	}
	s.Versions = maps.Clone(s.Versions)
	if s.Versions == nil {
		s.Versions = map[string][]string{}
	}
	s.Versions[OllamaEt] = slices.Clone(s.Versions[Ollama])
	if en {
		s.Templates, s.Versions = s.with(OllamaEt, WithSing(t))
	}
}

// Live is brain's template to render, completed: the saved one if it passes Validate, else Default (logged).
func (s Store) Live(brain string) string {
	t, ok := s.Templates[brain]
	if ok {
		if err := validate(brain, t, s.user(brain, false)); err != nil {
			logf("myvector prompt: saved " + brain + " template: " + err.Error() + "; using the built-in one")
			t = Default(brain)
		}
	} else {
		t = Default(brain)
	}
	t, _ = Complete(t)
	return t
}

// User is brain's {user} block (userFor) from the saved about-mes; one over AboutCap is dropped (logged).
func (s Store) User(brain string) string { return s.user(brain, true) }

func (s Store) user(brain string, log bool) string {
	fit := func(what, a string) string {
		if err := CheckAbout(a); err != nil {
			if log {
				logf("myvector prompt: saved " + what + ": " + err.Error() + "; left out")
			}
			return ""
		}
		return a
	}
	et := ""
	if brain == OllamaEt {
		et = fit("Estonian about-me", s.AboutEt)
	}
	return userFor(brain, fit("about-me", s.About), et)
}

// userFor is brain's {user} block: OllamaEt's is aboutEt when set, every other one (and an empty aboutEt) about.
func userFor(brain, about, aboutEt string) string {
	if brain == OllamaEt && strings.TrimSpace(aboutEt) != "" {
		return UserBlockEt(aboutEt)
	}
	return UserBlock(about)
}

// UserBlock is the {user} block: a lead-in line, then the about-me text; empty about renders empty.
func UserBlock(about string) string { return block("About the person you are talking with:", about) }

// UserBlockEt is UserBlock with an Estonian lead-in, for OllamaEt's AboutEt.
func UserBlockEt(about string) string { return block("Inimene, kellega räägid:", about) }

func block(head, about string) string {
	if a := strings.TrimSpace(about); a != "" {
		return head + "\n" + a
	}
	return ""
}

// CheckAbout rejects an about-me over AboutCap runes.
func CheckAbout(about string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(about)); n > AboutCap {
		return Invalid{fmt.Errorf("about-me is %d characters, over the %d limit", n, AboutCap)}
	}
	return nil
}

// Complete appends each RequiredTags tag tmpl lacks, so a block never drops; appended lists them for the page.
func Complete(tmpl string) (out string, appended []string) {
	for _, tag := range RequiredTags {
		if !strings.Contains(tmpl, tag) {
			appended = append(appended, tag)
		}
	}
	if len(appended) == 0 {
		return tmpl, nil
	}
	return strings.TrimRight(tmpl, " \t\n") + "\n\n" + strings.Join(appended, "\n\n"), appended
}

var tagRe = regexp.MustCompile(`\{[A-Za-z_]+\}`)

// Validate rejects an over-cap about, an unknown brain, an unknown {tag} (the {{cmd||param}} grammar is not a tag),
// and a render with UserBlock(about) over brain's Budget in either language.
func Validate(brain, tmpl, about string) error {
	if err := CheckAbout(about); err != nil {
		return err
	}
	return validate(brain, tmpl, UserBlock(about))
}

// validate is Validate with the rendered {user} block, so OllamaEt is checked with its own.
func validate(brain, tmpl, user string) error {
	if !Known(brain) {
		return Invalid{fmt.Errorf("unknown brain %q", brain)}
	}
	for _, tag := range tagRe.FindAllString(tmpl, -1) {
		if !slices.Contains(KnownTags, tag) {
			return Invalid{fmt.Errorf("unknown tag %s", tag)}
		}
	}
	full, _ := Complete(tmpl)
	for _, l := range []string{lang.Estonian, lang.English} {
		v := Sample(brain, l)
		v.User = user
		if n := Estimate(Render(full, v)); n > Budget(brain) {
			return Invalid{fmt.Errorf("%d tokens, over the %s budget of %d", n, brain, Budget(brain))}
		}
	}
	return nil
}

var mu sync.Mutex

// Save makes tmpl brain's live template, about the about-me and aboutEt OllamaEt's, each only when non-nil (the page
// sends what changed). Each brain whose template or {user} block changes must fit; one write, so a rejection changes nothing.
func Save(brain string, tmpl, about, aboutEt *string) error {
	if !Known(brain) {
		return Invalid{fmt.Errorf("unknown brain %q", brain)}
	}
	kv := map[string]any{}
	for _, f := range []struct {
		key string
		p   *string
	}{{"about", about}, {"about_et", aboutEt}} {
		if f.p != nil {
			a := strings.TrimSpace(*f.p)
			if err := CheckAbout(a); err != nil {
				return err
			}
			kv[f.key] = a
		}
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := load()
	if err != nil {
		return err
	}
	next := s
	if a, ok := kv["about"].(string); ok {
		next.About = a
	}
	if a, ok := kv["about_et"].(string); ok {
		next.AboutEt = a
	}
	for _, b := range Brains {
		sent := b == brain && tmpl != nil
		user := next.user(b, false)
		if !sent && user == s.user(b, false) {
			continue // neither changed for b
		}
		t, ok := s.Templates[b]
		if sent {
			t, ok = *tmpl, true
		}
		if !ok {
			t = Default(b)
		}
		if err := validate(b, t, user); err != nil {
			if !sent { // a saved template, not the one sent: name it
				return fmt.Errorf("saved %s template: %w", b, err)
			}
			return err
		}
	}
	if tmpl != nil {
		kv["templates"], kv["versions"] = s.with(brain, *tmpl)
	}
	if len(kv) == 0 {
		return nil
	}
	return conf.Update(Path(), kv)
}

// Restore re-saves brain's version i (0 = newest).
func Restore(brain string, i int) error {
	mu.Lock()
	defer mu.Unlock()
	s, err := load()
	if err != nil {
		return err
	}
	v := s.Versions[brain]
	if i < 0 || i >= len(v) {
		return Invalid{fmt.Errorf("no %s version %d", brain, i)}
	}
	if err := validate(brain, v[i], s.user(brain, false)); err != nil {
		return err
	}
	t, vs := s.with(brain, v[i])
	return conf.Update(Path(), map[string]any{"templates": t, "versions": vs})
}

// Reset drops brain's saved template, so the built-in default goes live; versions are kept.
func Reset(brain string) error {
	if !Known(brain) {
		return Invalid{fmt.Errorf("unknown brain %q", brain)}
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := load()
	if err != nil {
		return err
	}
	t := maps.Clone(s.Templates)
	delete(t, brain)
	kv := map[string]any{"templates": t}
	if len(s.Versions) > 0 { // a seeded OllamaEt's versions, so it stays reset
		kv["versions"] = s.Versions
	}
	return conf.Update(Path(), kv)
}

// with is s's templates and versions after tmpl becomes brain's live one (newest version first, keepVersions kept).
func (s Store) with(brain, tmpl string) (map[string]string, map[string][]string) {
	t, v := maps.Clone(s.Templates), maps.Clone(s.Versions)
	if t == nil {
		t = map[string]string{}
	}
	if v == nil {
		v = map[string][]string{}
	}
	t[brain] = tmpl
	if h := v[brain]; len(h) == 0 || h[0] != tmpl {
		v[brain] = append([]string{tmpl}, h...)
	}
	if len(v[brain]) > keepVersions {
		v[brain] = v[brain][:keepVersions]
	}
	return t, v
}
