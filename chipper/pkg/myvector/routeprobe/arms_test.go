package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// Fictional profile only (et-prompt-tuning.md (docs/plans/) §Metrics).
const testAbout = "Mina olen Mari Tamm. Elan Vesikülas koos koer Muriga."

// savedLike is the shape of a saved ollama template whose length line is not the default's.
const savedLike = "You are Vector, a playful companion robot. Reply in short sentences unless asked for more. Vary how you begin.\n\n" +
	"{user}\n\n{facts}\n\n{diary}\n\n{search}\n\n{speech}\n\n{commands}\n\n{now}\nDo not state the time unless asked.\n\n{lang}"

func withStore(t *testing.T, tmpl string) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "myvectorPrompt.json")
	old := prompt.Path
	prompt.Path = func() string { return p }
	t.Cleanup(func() { prompt.Path = old; llm.ProbeSampling = nil })
	b, _ := json.Marshal(prompt.Store{Templates: map[string]string{prompt.Ollama: tmpl}, About: testAbout})
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPromptArms(t *testing.T) {
	tempt := ask{"et-EE", "tempt", "Räägi üks lugu robotist."}
	self := ask{"et-EE", "self", "Kus ma elan?"}
	tokens := profileTokens(testAbout, nil)
	for _, tmpl := range []string{prompt.Default(prompt.Ollama), savedLike} {
		raw := withStore(t, tmpl)
		base, err := readBase(tokens)
		if err != nil {
			t.Fatal(err)
		}
		for _, pa := range promptArms {
			if err := base.apply(pa, tempt); err != nil {
				t.Fatalf("%s: %v", pa.id, err)
			}
			got, _ := os.ReadFile(prompt.Path())
			s := prompt.Load()
			if err := prompt.Validate(prompt.Ollama, s.Templates[prompt.Ollama], s.About); err != nil {
				t.Errorf("arm %s: %v", pa.id, err)
			}
			changed := s.Live(prompt.Ollama) != base.tmpl
			if changed != (pa.tmpl != nil) {
				t.Errorf("arm %s: template changed %v, has a rewrite %v", pa.id, changed, pa.tmpl != nil)
			}
			if pa.id == "0" && !bytes.Equal(got, raw) {
				t.Errorf("arm 0 changed the store")
			}
			sampled := llm.SamplingFor(llm.Ollama, "qwen3.5:4b").Family == "probe-S"
			if sampled != (pa.id == "S") {
				t.Errorf("arm %s: probe sampling %v", pa.id, sampled)
			}
			wantAbout := testAbout
			if pa.about != nil {
				wantAbout = ""
			}
			if s.About != wantAbout {
				t.Errorf("arm %s on a tempt ask: about-me %q, want %q", pa.id, s.About, wantAbout)
			}
		}
		// about-me kept per arm on: a self ask, a "to me" tempt ask
		for _, c := range []struct {
			id         string
			self, toMe bool
		}{{"G", true, true}, {"G2", true, false}, {"N", false, false}} {
			pa := promptArms[slices.IndexFunc(promptArms, func(a promptArm) bool { return a.id == c.id })]
			for _, k := range []struct {
				a    ask
				keep bool
			}{{self, c.self}, {ask{"et-EE", "tempt", "Räägi mulle üks lugu."}, c.toMe}} {
				if err := base.apply(pa, k.a); err != nil {
					t.Fatal(err)
				}
				if got := prompt.Load().About; (got == testAbout) != k.keep {
					t.Errorf("arm %s on %q: about-me %q, keep %v", c.id, k.a.Text, got, k.keep)
				}
			}
		}
		combos, err := parsePrompt("S+R,D+G2,P+R")
		if err != nil {
			t.Fatal(err)
		}
		for _, pa := range combos {
			if err := base.apply(pa, tempt); err != nil {
				t.Fatalf("%s: %v", pa.id, err)
			}
			s := prompt.Load()
			if err := prompt.Validate(prompt.Ollama, s.Templates[prompt.Ollama], s.About); err != nil {
				t.Errorf("arm %s: %v", pa.id, err)
			}
			live, sampled := s.Live(prompt.Ollama), llm.SamplingFor(llm.Ollama, "qwen3.5:4b").Family == "probe-S"
			switch pa.id {
			case "S+R":
				if !sampled || !strings.HasSuffix(strings.TrimSpace(live), lineR) || s.About != testAbout {
					t.Errorf("S+R: sampled %v, R last %v, about-me %q", sampled, strings.HasSuffix(strings.TrimSpace(live), lineR), s.About)
				}
			case "D+G2":
				if sampled || !strings.HasPrefix(live, headerD) || s.About != "" {
					t.Errorf("D+G2: sampled %v, D header %v, about-me %q", sampled, strings.HasPrefix(live, headerD), s.About)
				}
			case "P+R":
				if !strings.Contains(live, lineP) || !strings.Contains(live, lineR) {
					t.Errorf("P+R: P line %v, R line %v", strings.Contains(live, lineP), strings.Contains(live, lineR))
				}
			default:
				t.Errorf("combo id %q", pa.id)
			}
		}
	}
}

// TestPromptArmsLongAbout: every arm fits prompt.Budget with an about-me at prompt.AboutCap.
func TestPromptArmsLongAbout(t *testing.T) {
	long := strings.Repeat("Mari Tamm elab Vesikülas. ", 20)[:prompt.AboutCap]
	for _, tmpl := range []string{prompt.Default(prompt.Ollama), savedLike} {
		withStore(t, tmpl)
		if err := prompt.Save(prompt.Ollama, nil, &long); err != nil {
			t.Fatal(err)
		}
		base, err := readBase(nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := base.check(promptArms); err != nil {
			t.Errorf("%v", err)
		}
	}
}

func TestParsePrompt(t *testing.T) {
	if a, err := parsePrompt(""); err != nil || len(a) != 1 || a[0].id != "0" {
		t.Errorf(`"" = %v, %v; want arm 0`, a, err)
	}
	if a, err := parsePrompt("0, P,G"); err != nil || len(a) != 3 || a[2].id != "G" {
		t.Errorf("0,P,G = %v, %v", a, err)
	}
	if a, err := parsePrompt("0,S+R,D+G"); err != nil || len(a) != 3 || a[1].id != "S+R" || a[2].id != "D+G" {
		t.Errorf("0,S+R,D+G = %v, %v", a, err)
	}
	for _, bad := range []string{"Q", "0,0", "S+R,S+R", "S+Q", "0+R", "G+N", "S+S", "R+R", "D+X", "D+P"} {
		if _, err := parsePrompt(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestProfileTokens(t *testing.T) {
	mem := &memory.Store{
		Profiles: map[string]memory.Profile{"esn": {Text: "They have a cat called Nupu."}},
		Facts: []memory.Fact{{Text: "Works at Telia.", Status: memory.Active},
			{Text: "Used to live in Pärnu.", Status: memory.Removed}},
	}
	got := profileTokens(testAbout+" Vector on mu sõber.", mem)
	want := []string{"mari", "tamm", "vesikülas", "muriga", "nupu", "telia"} // Elan, Works: sentence starters only
	if !slices.Equal(got, want) {
		t.Errorf("profileTokens = %v, want %v", got, want)
	}
	if h := hits("Vesikülast on Tallinnani pikk maa.", got); !slices.Equal(h, []string{"vesikülas"}) {
		t.Errorf("hits = %v", h)
	}
	if h := hits("Tammsaare kirjutas selle, Tammega koos.", []string{"tamm"}); len(h) != 1 {
		t.Errorf("Tammega (case ending) should hit tamm: %v", h)
	}
	if h := hits("Tammsaare kirjutas selle.", []string{"tamm"}); len(h) != 0 {
		t.Errorf("Tammsaare is another word, not tamm: %v", h)
	}
	if got := profileTokens("Mari elab siin. Ta on Mari Tamm.", nil); !slices.Equal(got, []string{"mari", "tamm"}) {
		t.Errorf("a starter also capitalised mid-sentence is a token: %v", got)
	}
}
