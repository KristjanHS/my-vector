package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := Save(Ollama, "Be brief."); err != nil {
		t.Fatal(err)
	}
	if live := Load().Live(Ollama); !strings.Contains(live, "{commands}") {
		t.Errorf("live template lost {commands}: %q", live)
	}
}

func TestValidateBudget(t *testing.T) {
	for _, b := range []string{Claude, Ollama} {
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
	if err := SaveAbout(strings.Repeat("ä", AboutCap)); err != nil {
		t.Errorf("at cap: %v", err)
	}
	if err := SaveAbout(strings.Repeat("ä", AboutCap+1)); err == nil {
		t.Error("over cap accepted")
	}
	if got := Load().User(); !strings.HasPrefix(got, "About the person you are talking with:\n") {
		t.Errorf("user block = %q", got)
	}
	if got := (Store{About: strings.Repeat("a", AboutCap+1)}).User(); got != "" || len(*logs) != 1 {
		t.Errorf("hand-edited over-cap about: block %q, logs %v", got, *logs)
	}
	if UserBlock("  ") != "" {
		t.Error("empty about rendered a block")
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
		if err := Save(Claude, fmt.Sprintf("v%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	Save(Claude, "v6") // same as newest: no entry
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
	Save(Claude, "mine")
	Save(Ollama, "theirs")
	SaveAbout("Mari")
	if err := Reset(Claude); err != nil {
		t.Fatal(err)
	}
	s := Load()
	if s.Live(Claude) != Default(Claude) || s.Templates[Ollama] != "theirs" || s.About != "Mari" || len(s.Versions[Claude]) != 1 {
		t.Errorf("after reset: %+v", s)
	}
}
