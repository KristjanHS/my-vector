package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func TestScore(t *testing.T) {
	for _, c := range []struct {
		answer string
		words  []string
		want   float64
	}{
		{"A cup on a table.", []string{"cup", "table"}, 1},
		{"Nothing here.", []string{"cup", "table"}, 0},
		{"Laual on kaks Tassid.", []string{"tass"}, 1},
		{"A CUP.", []string{"Cup", "table"}, 0.5},
		{"teacup", []string{"cup"}, 0},
	} {
		if got := score(c.answer, c.words); got != c.want {
			t.Errorf("score(%q, %v) = %v, want %v", c.answer, c.words, got, c.want)
		}
	}
}

func TestLoadChecklist(t *testing.T) {
	write := func(t *testing.T, list string, photos ...string) string {
		d := t.TempDir()
		for _, p := range photos {
			os.WriteFile(filepath.Join(d, p), []byte{0xff, 0xd8}, 0o644)
		}
		os.WriteFile(filepath.Join(d, "checklist.json"), []byte(list), 0o644)
		return d
	}
	ok := `[{"photo":"01.jpg","et":["tass"],"en":["cup"]}]`
	if items, err := loadChecklist(write(t, ok, "01.jpg")); err != nil || len(items) != 1 || items[0].ET[0] != "tass" {
		t.Fatalf("good checklist: %v %v", items, err)
	}
	for name, c := range map[string]struct{ list, want string }{
		"missing photo": {ok, "01.jpg"},
		"empty et":      {`[{"photo":"01.jpg","et":[],"en":["cup"]}]`, "non-empty"},
		"no en":         {`[{"photo":"01.jpg","et":["tass"]}]`, "non-empty"},
		"two words":     {`[{"photo":"01.jpg","et":["tass"],"en":["coffee cup"]}]`, "one word"},
		"empty word":    {`[{"photo":"01.jpg","et":[""],"en":["cup"]}]`, "one word"},
	} {
		photos := []string{"01.jpg"}
		if name == "missing photo" {
			photos = nil
		}
		if _, err := loadChecklist(write(t, c.list, photos...)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want it to name %q", name, err, c.want)
		}
	}
}

func TestArmPrompt(t *testing.T) {
	a, b := armSystem("a"), armSystem("b")
	if !strings.Contains(a, prompt.SpeechBlock(prompt.OllamaEt)) || strings.Contains(a, prompt.LangLine(prompt.Ollama, lang.Estonian)) {
		t.Errorf("arm a is not the et persona render:\n%s", a)
	}
	if !strings.Contains(b, prompt.LangLine(prompt.Ollama, lang.Estonian)) || strings.Contains(b, prompt.SpeechBlock(prompt.OllamaEt)) {
		t.Errorf("arm b is not the English persona + Estonian line:\n%s", b)
	}
	r := relayRequest(llm.Target{Provider: llm.Ollama, Model: "apertus-estllm:8b"}, "A white cup on a desk.")
	if len(r.Messages) != 2 || !strings.Contains(r.Messages[1].Content, "A white cup on a desk.") ||
		!strings.Contains(r.Messages[0].Content, prompt.SpeechBlock(prompt.OllamaEt)) || r.Messages[1].MultiContent != nil {
		t.Errorf("arm c hop 2 is not text-only to the et brain with hop 1 inside: %+v", r.Messages)
	}
}
