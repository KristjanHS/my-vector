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

// AboutCap is the about-me limit in runes (≈150 tokens), shared by both brains.
const AboutCap = 450

const keepVersions = 5

// RequiredTags are appended, in this (the default template's) order, to a template that lacks them (Complete);
// the other KnownTags are optional.
var (
	RequiredTags = []string{"{speech}", "{commands}", "{now}", "{lang}"}
	KnownTags    = []string{"{now}", "{lang}", "{user}", "{facts}", "{diary}", "{search}", "{speech}", "{commands}"}
)

// Budget is the token ceiling for brain's template rendered with Sample, empty facts/diary/search and the about-me.
func Budget(brain string) int {
	if brain == Ollama {
		return 600
	}
	return 2000
}

// SearchBudget is the token ceiling for brain's {search} block (plan web-search W9).
func SearchBudget(brain string) int {
	if brain == Ollama {
		return 300
	}
	return 800
}

// MemoryBudget is the token ceiling for brain's {facts} + {diary} blocks together (memory.Block fits it).
func MemoryBudget(brain string) int {
	if brain == Ollama {
		return 300
	}
	return 3000
}

var logf = logger.Println

// Invalid marks a rejected brain, template, about-me or version (the page's 400); any other store error is I/O.
type Invalid struct{ error }

// Store is myvectorPrompt.json: per-brain saved template and versions (newest first), and the about-me text.
type Store struct {
	Templates map[string]string   `json:"templates,omitempty"`
	Versions  map[string][]string `json:"versions,omitempty"`
	About     string              `json:"about,omitempty"`
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
	return s, nil
}

// Live is brain's template to render, completed: the saved one if it passes Validate, else Default (logged).
func (s Store) Live(brain string) string {
	t, ok := s.Templates[brain]
	if ok {
		if err := Validate(brain, t, s.about()); err != nil {
			logf("myvector prompt: saved " + brain + " template: " + err.Error() + "; using the built-in one")
			t = Default(brain)
		}
	} else {
		t = Default(brain)
	}
	t, _ = Complete(t)
	return t
}

// User is the {user} block for the saved about-me; one over AboutCap is dropped (logged).
func (s Store) User() string {
	if err := CheckAbout(s.About); err != nil {
		logf("myvector prompt: saved about-me: " + err.Error() + "; left out")
		return ""
	}
	return UserBlock(s.About)
}

func (s Store) about() string {
	if CheckAbout(s.About) != nil {
		return ""
	}
	return s.About
}

// UserBlock is the {user} block: a lead-in line, then the about-me text; empty about renders empty.
func UserBlock(about string) string {
	if a := strings.TrimSpace(about); a != "" {
		return "About the person you are talking with:\n" + a
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

// Validate rejects an unknown brain, an unknown {tag} (the {{cmd||param}} grammar is not a tag), and a render over
// brain's Budget in either language.
func Validate(brain, tmpl, about string) error {
	if brain != Claude && brain != Ollama {
		return Invalid{fmt.Errorf("unknown brain %q", brain)}
	}
	for _, tag := range tagRe.FindAllString(tmpl, -1) {
		if !slices.Contains(KnownTags, tag) {
			return Invalid{fmt.Errorf("unknown tag %s", tag)}
		}
	}
	if err := CheckAbout(about); err != nil {
		return err
	}
	full, _ := Complete(tmpl)
	for _, l := range []string{lang.Estonian, lang.English} {
		v := Sample(l)
		v.User = UserBlock(about)
		if n := Estimate(Render(full, v)); n > Budget(brain) {
			return Invalid{fmt.Errorf("%d tokens, over the %s budget of %d", n, brain, Budget(brain))}
		}
	}
	return nil
}

var mu sync.Mutex

// Save makes tmpl brain's live template and about the about-me, each only when non-nil (the page sends what changed).
// Both brains' resulting templates must fit the resulting about-me; it is one write, so a rejected save changes nothing.
func Save(brain string, tmpl, about *string) error {
	if brain != Claude && brain != Ollama {
		return Invalid{fmt.Errorf("unknown brain %q", brain)}
	}
	var a string
	if about != nil {
		a = strings.TrimSpace(*about)
		if err := CheckAbout(a); err != nil {
			return err
		}
	}
	mu.Lock()
	defer mu.Unlock()
	s, err := load()
	if err != nil {
		return err
	}
	kv := map[string]any{}
	if about == nil {
		a = s.about()
	} else {
		kv["about"] = a
	}
	for _, b := range []string{Claude, Ollama} {
		t, ok := s.Templates[b]
		if b == brain && tmpl != nil {
			t, ok = *tmpl, true
		} else if about == nil {
			continue // neither changed for b
		}
		if !ok {
			t = Default(b)
		}
		if err := Validate(b, t, a); err != nil {
			if b != brain {
				return fmt.Errorf("%s template: %w", b, err)
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
	if err := Validate(brain, v[i], s.about()); err != nil {
		return err
	}
	t, vs := s.with(brain, v[i])
	return conf.Update(Path(), map[string]any{"templates": t, "versions": vs})
}

// Reset drops brain's saved template, so the built-in default goes live; versions are kept.
func Reset(brain string) error {
	if brain != Claude && brain != Ollama {
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
	return conf.Update(Path(), map[string]any{"templates": t})
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
