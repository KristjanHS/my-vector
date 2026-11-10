package prompt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func ptr(s string) *string { return &s }

// useStore points Path at a temp file for t and captures log lines.
func useStore(t *testing.T) (path string, logs *[]string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "myvectorPrompt.json")
	oldPath, oldLog := Path, logf
	var got []string
	Path = func() string { return path }
	logf = func(a ...any) { got = append(got, fmt.Sprint(a...)) }
	t.Cleanup(func() { Path, logf = oldPath, oldLog })
	return path, &got
}

func TestValidateUnknownTag(t *testing.T) {
	if err := Validate(Claude, Default(Claude)+"\n{weather}", ""); err == nil || !strings.Contains(err.Error(), "{weather}") {
		t.Errorf("unknown tag: err = %v", err)
	}
	if err := Validate(Claude, Default(Claude)+"\nSay {{playAnimationWI||happy}} once.", ""); err != nil {
		t.Errorf("command grammar flagged as a tag: %v", err)
	}
	if err := Validate("gpt", Default(Claude), ""); err == nil {
		t.Error("unknown brain accepted")
	}
}

func TestRenderAppendsMissing(t *testing.T) {
	got, appended := Complete("Be brief.\n\n{now}\n{speech}\n")
	if strings.Join(appended, " ") != "{commands} {lang}" {
		t.Errorf("appended = %v", appended)
	}
	if !strings.HasSuffix(got, "{speech}\n\n{commands}\n\n{lang}") {
		t.Errorf("completed template = %q", got)
	}
	if _, appended := Complete(Default(Ollama)); appended != nil {
		t.Errorf("default flagged: %v", appended)
	}
	_, _ = useStore(t)
	if err := Save(Ollama, ptr("Be brief."), nil, nil); err != nil {
		t.Fatal(err)
	}
	if live := Load().Live(Ollama); !strings.Contains(live, "{commands}") {
		t.Errorf("live template lost {commands}: %q", live)
	}
}

func TestValidateBudget(t *testing.T) {
	for _, b := range Brains {
		if err := Validate(b, Default(b), strings.Repeat("a", AboutCap)); err != nil {
			t.Errorf("%s default with a full about-me: %v", b, err)
		}
		pad := strings.Repeat("x", 3*Budget(b))
		if err := Validate(b, Default(b)+pad, ""); err == nil || !strings.Contains(err.Error(), "budget") {
			t.Errorf("%s over budget: err = %v", b, err)
		}
	}
}

func TestAboutCap(t *testing.T) {
	_, logs := useStore(t)
	if err := Save(Claude, nil, ptr(strings.Repeat("ä", AboutCap)), nil); err != nil {
		t.Errorf("at cap: %v", err)
	}
	if err := Save(Claude, nil, ptr(strings.Repeat("ä", AboutCap+1)), nil); err == nil {
		t.Error("over cap accepted")
	}
	if got := Load().User(Claude); !strings.HasPrefix(got, "About the person you are talking with:\n") {
		t.Errorf("user block = %q", got)
	}
	if got := (Store{About: strings.Repeat("a", AboutCap+1)}).User(Claude); got != "" || len(*logs) != 1 {
		t.Errorf("hand-edited over-cap about: block %q, logs %v", got, *logs)
	}
	if UserBlock("  ") != "" {
		t.Error("empty about rendered a block")
	}
}

// R8/R9: only ollama-et renders AboutEt, under the Estonian heading; empty AboutEt falls back to About.
func TestAboutEtOllamaEtOnly(t *testing.T) {
	_, logs := useStore(t)
	if err := Save(OllamaEt, nil, ptr("Mari"), ptr(" Mari elab Tartus. ")); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if got := s.User(OllamaEt); got != "Inimene, kellega räägid:\nMari elab Tartus." {
		t.Errorf("ollama-et user block = %q", got)
	}
	for _, b := range []string{Claude, Ollama} {
		if got := s.User(b); got != "About the person you are talking with:\nMari" {
			t.Errorf("%s user block = %q", b, got)
		}
	}
	if err := Save(Claude, nil, nil, ptr("")); err != nil {
		t.Fatal(err)
	}
	if got := Load().User(OllamaEt); got != "About the person you are talking with:\nMari" {
		t.Errorf("empty AboutEt: ollama-et user block = %q", got)
	}
	if Save(OllamaEt, nil, nil, ptr(strings.Repeat("ä", AboutCap+1))) == nil {
		t.Error("over-cap AboutEt accepted")
	}
	over := Store{About: "Mari", AboutEt: strings.Repeat("a", AboutCap+1)}
	if got := over.User(OllamaEt); got != UserBlock("Mari") || len(*logs) != 1 {
		t.Errorf("hand-edited over-cap AboutEt: block %q, logs %v", got, *logs)
	}
}

// Save validates only the brains whose {user} block changes, each with its own block.
func TestSaveAboutScope(t *testing.T) {
	path, _ := useStore(t)
	os.WriteFile(path, []byte(`{"templates":{"claude":"Hi {weather}"}}`), 0o600)
	if err := Save(OllamaEt, nil, nil, ptr("Mari")); err != nil {
		t.Errorf("AboutEt change validated claude's broken template: %v", err)
	}
	if err := Save(OllamaEt, nil, ptr("Mari"), nil); err == nil || !strings.Contains(err.Error(), "saved claude template") {
		t.Errorf("About change skipped claude: err = %v", err)
	}
	os.WriteFile(path, []byte(`{"about_et":"Mari"}`), 0o600)
	full := Default(OllamaEt) + "\n"
	for validate(OllamaEt, full+"x", UserBlockEt("Mari")) == nil { // pad to the budget's edge
		full += "x"
	}
	if err := Save(OllamaEt, &full, nil, nil); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", AboutCap)
	if err := Save(Claude, nil, ptr(long), nil); err != nil {
		t.Errorf("About change blocked though ollama-et renders AboutEt: %v", err)
	}
	for name, et := range map[string]string{"long AboutEt": long, "cleared AboutEt (falls back to the long About)": ""} {
		err := Save(Claude, nil, nil, &et)
		if !errors.As(err, new(Invalid)) || !strings.Contains(err.Error(), "saved ollama-et template") {
			t.Errorf("%s over ollama-et's budget: err = %v", name, err)
		}
	}
	if s := Load(); s.AboutEt != "Mari" || s.About != long {
		t.Errorf("a rejected save wrote: about_et %q", s.AboutEt)
	}
	if err := Save(Claude, nil, nil, ptr("Mai")); err != nil {
		t.Errorf("AboutEt change checked against the long About, not its own block: %v", err)
	}
}

func TestLoadFallback(t *testing.T) {
	path, logs := useStore(t)
	if s := Load(); s.Live(Claude) != Default(Claude) || len(*logs) != 0 {
		t.Errorf("missing file: logs %v", *logs)
	}
	os.WriteFile(path, []byte("{not json"), 0o644)
	if s := Load(); s.Live(Claude) != Default(Claude) || len(*logs) != 1 {
		t.Errorf("bad file: logs %v", *logs)
	}
	os.WriteFile(path, []byte(`{"templates":{"ollama":"Hi {weather}","claude":"Hi."}}`), 0o644)
	s := Load()
	if s.Live(Ollama) != Default(Ollama) || len(*logs) != 2 || !strings.Contains((*logs)[1], "{weather}") {
		t.Errorf("bad saved template: logs %v", *logs)
	}
	if !strings.HasPrefix(s.Live(Claude), "Hi.") {
		t.Errorf("the good brain fell back too: %q", s.Live(Claude))
	}
}

func TestVersionsKeepFive(t *testing.T) {
	_, _ = useStore(t)
	for i := 0; i < 7; i++ {
		if err := Save(Claude, ptr(fmt.Sprintf("v%d", i)), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	Save(Claude, ptr("v6"), nil, nil) // same as newest: no entry
	if got := strings.Join(Load().Versions[Claude], " "); got != "v6 v5 v4 v3 v2" {
		t.Errorf("versions = %s", got)
	}
	if err := Restore(Claude, 3); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Templates[Claude] != "v3" || strings.Join(s.Versions[Claude], " ") != "v3 v6 v5 v4 v3" {
		t.Errorf("after restore: live %q, versions %v", s.Templates[Claude], s.Versions[Claude])
	}
	if Restore(Claude, 5) == nil || len(s.Versions[Ollama]) != 0 {
		t.Error("restore out of range accepted, or ollama got claude's versions")
	}
}

func TestReset(t *testing.T) {
	_, _ = useStore(t)
	Save(Claude, ptr("mine"), nil, nil)
	Save(Ollama, ptr("theirs"), nil, nil)
	Save(Claude, nil, ptr("Mari"), nil)
	if err := Reset(Claude); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Live(Claude) != Default(Claude) || s.Templates[Ollama] != "theirs" || s.About != "Mari" || len(s.Versions[Claude]) != 1 {
		t.Errorf("after reset: %+v", s)
	}
}

func TestSeedOllamaEt(t *testing.T) {
	path, _ := useStore(t)
	os.WriteFile(path, []byte(`{"templates":{"claude":"c","ollama":"o"},"versions":{"ollama":["o","o1"]}}`), 0o600)
	s := Load() // no anchor in "o": seeded as is
	if s.Templates[OllamaEt] != "o" || strings.Join(s.Versions[OllamaEt], " ") != "o o1" || s.Templates[Ollama] != "o" {
		t.Fatalf("seeded: %+v", s)
	}
	os.WriteFile(path, []byte(`{"templates":{"ollama":"Hi. `+singAnchor+` Bye."}}`), 0o600)
	if s = Load(); s.Templates[OllamaEt] != "Hi. "+singAnchor+" "+SingEt+" Bye." || len(s.Versions[OllamaEt]) != 1 {
		t.Fatalf("seeded: %+v", s)
	}
	if err := Save(Claude, ptr("c2"), nil, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"ollama-et"`) {
		t.Fatalf("seed not persisted by the next write: %s", b)
	}
	if err := Reset(OllamaEt); err != nil {
		t.Fatal(err)
	}
	if s := Load(); s.Live(OllamaEt) != Default(OllamaEt) || len(s.Versions[OllamaEt]) != 1 {
		t.Errorf("reset ollama-et re-seeded: %+v", s)
	}
	os.WriteFile(path, []byte(`{"templates":{"ollama":"o","ollama-et":"e"}}`), 0o600)
	if s := Load(); s.Templates[OllamaEt] != "e" {
		t.Errorf("saved ollama-et overwritten by the seed: %+v", s)
	}
}

func TestCorruptStoreNotOverwritten(t *testing.T) {
	path, _ := useStore(t)
	writes := map[string]func() error{
		"Save":       func() error { return Save(Claude, ptr("Hi."), nil, nil) },
		"Save about": func() error { return Save(Claude, nil, ptr("Mari"), nil) },
		"Reset":      func() error { return Reset(Claude) },
		"Restore":    func() error { return Restore(Claude, 0) },
	}
	for _, bad := range []string{`{not json`, `{"versions":{"claude":"x"},"about":"keep me"}`} {
		for name, write := range writes {
			os.WriteFile(path, []byte(bad), 0o644)
			err := write()
			if err == nil || errors.As(err, new(Invalid)) || !strings.Contains(err.Error(), "fix or delete it") {
				t.Errorf("%s on %s: err = %v, want a non-Invalid fix-or-delete error", name, bad, err)
			}
			if b, _ := os.ReadFile(path); string(b) != bad {
				t.Errorf("%s on %s rewrote the file: %s", name, bad, b)
			}
		}
	}
}

// Every tag renders at least as long in et-EE as in en-US, so only an Estonian-only overrun is constructible.
func TestValidateBudgetBothLanguages(t *testing.T) {
	render := func(tmpl, l string) string {
		v := Sample(Ollama, l)
		v.User = ""
		return Render(tmpl, v)
	}
	tmpl := Default(Ollama) + "\n"
	pad := 3*Budget(Ollama) + 1 - utf8.RuneCountInString(render(tmpl, lang.Estonian))
	tmpl += strings.Repeat("x", pad)
	et, en := Estimate(render(tmpl, lang.Estonian)), Estimate(render(tmpl, lang.English))
	if et != Budget(Ollama)+1 || en > Budget(Ollama) {
		t.Fatalf("setup: et-EE %d en-US %d tokens, want %d and <= %d", et, en, Budget(Ollama)+1, Budget(Ollama))
	}
	if err := Validate(Ollama, tmpl, ""); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Errorf("over budget in et-EE only: err = %v", err)
	}
}
