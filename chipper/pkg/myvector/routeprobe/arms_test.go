package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
			if pa.id == "G" || pa.id == "N" {
				wantAbout = ""
			}
			if s.About != wantAbout {
				t.Errorf("arm %s on a tempt ask: about-me %q, want %q", pa.id, s.About, wantAbout)
			}
		}
		for _, pa := range promptArms {
			if pa.id != "G" && pa.id != "N" {
				continue
			}
			if err := base.apply(pa, self); err != nil {
				t.Fatal(err)
			}
			if got, want := prompt.Load().About, map[string]string{"G": testAbout, "N": ""}[pa.id]; got != want {
				t.Errorf("arm %s on a self ask: about-me %q, want %q", pa.id, got, want)
			}
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
	for _, bad := range []string{"Q", "0,0"} {
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
	want := []string{"mari", "tamm", "elan", "vesikülas", "muriga", "nupu", "works", "telia"}
	if !slices.Equal(got, want) {
		t.Errorf("profileTokens = %v, want %v", got, want)
	}
	if h := hits("Vesikülast on Tallinnani pikk maa.", got); !slices.Equal(h, []string{"vesikülas"}) {
		t.Errorf("hits = %v", h)
	}
}
