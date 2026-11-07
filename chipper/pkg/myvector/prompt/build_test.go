package prompt

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

func TestRenderGolden(t *testing.T) {
	for _, brain := range []string{Claude, Ollama} {
		for _, l := range []string{lang.Estonian, lang.English} {
			got := Render(Default(brain), Sample(l))
			path := filepath.Join("testdata", brain+"_"+l+".golden")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s %s: render differs from %s:\n%s", brain, l, path, got)
			}
			if et := strings.Contains(got, "Answer in"); et != (l == lang.Estonian) {
				t.Errorf("%s %s: language line present = %v", brain, l, et)
			}
		}
	}
}

func TestRenderDropsEmptyBlocks(t *testing.T) {
	got := Render(Default(Ollama), Vars{Now: "NOW", Speech: "SPEECH"})
	if want := persona + "\n\nSPEECH\n\nNOW"; got != want {
		t.Errorf("Render with empty blocks = %q, want %q", got, want)
	}
	// block text is not rescanned: a tag inside a block stays literal
	if got := Render("{commands}", Vars{Commands: "{{playAnimationWI||happy}} {now}"}); got != "{{playAnimationWI||happy}} {now}" {
		t.Errorf("block rescanned: %q", got)
	}
}

func TestCommandsBlockBudget(t *testing.T) {
	c := Commands(SampleEmotions, true)
	if n := Estimate(c); n > 170 {
		t.Errorf("commands block is %d tokens, cap 170", n)
	}
	if strings.Contains(Commands(SampleEmotions, false), "newVoiceRequest") {
		t.Error("newVoiceRequest offered without follow-up")
	}
	if strings.Contains(c, "lookingUp") {
		t.Error("getImage offers lookingUp")
	}
}

func TestEstimate(t *testing.T) {
	for s, want := range map[string]int{"": 0, "a": 1, "abc": 1, "abcd": 2, "ääää": 2} {
		if got := Estimate(s); got != want {
			t.Errorf("Estimate(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestReplyCap(t *testing.T) {
	if ReplyCap(Claude) != 400 || ReplyCap(Ollama) != 300 {
		t.Errorf("ReplyCap claude %d ollama %d", ReplyCap(Claude), ReplyCap(Ollama))
	}
}
