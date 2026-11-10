package relay

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

func TestTranslate(t *testing.T) {
	hop1 := "{{playAnimationWI||confused}} A white cup. {{getImage||front}} Like it?"
	d := Translate(llm.Target{Provider: llm.Ollama, Model: "apertus-estllm:8b"}, hop1)
	if last := d.Messages[len(d.Messages)-1].Content; last != Ask+"A white cup. Like it?" || d.Messages[0].Content != System {
		t.Errorf("hop 2 is not the translate ask + hop 1 without commands: %+v", d.Messages)
	}
	if got, want := Attach(hop1, "Valge tass."), "{{playAnimationWI||confused}} {{getImage||front}} Valge tass."; got != want {
		t.Errorf("Attach = %q, want %q", got, want)
	}
}

func TestToEnglish(t *testing.T) {
	r := ToEnglish(llm.Target{Provider: llm.Ollama, Model: "apertus-estllm:8b"}, " mis värvi on tass? ")
	if last := r.Messages[len(r.Messages)-1].Content; last != EnglishAsk+"mis värvi on tass?" || r.Messages[0].Content != EnglishSystem {
		t.Errorf("hop 0 is not the English ask + the trimmed question: %+v", r.Messages)
	}
}
