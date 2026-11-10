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
	for _, brain := range Brains {
		for _, l := range []string{lang.Estonian, lang.English} {
			got := Render(Default(brain), Sample(brain, l))
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
			if et := strings.HasSuffix(got, LangLine(brain, lang.Estonian)); et != (l == lang.Estonian) {
				t.Errorf("%s %s: language line present = %v", brain, l, et)
			}
		}
	}
}

func TestRenderDropsEmptyBlocks(t *testing.T) {
	got := Render(Default(Ollama), Vars{Now: "NOW", Speech: "SPEECH", User: " \n "})
	if want := persona + "\n\nSPEECH\n\nNOW"; got != want {
		t.Errorf("Render with empty blocks = %q, want %q", got, want)
	}
	// block text is not rescanned, whatever the tag order: each block names the other's tag
	if got := Render("{now}|{commands}", Vars{Now: "{commands}", Commands: "{{playAnimationWI||happy}} {now}"}); got != "{commands}|{{playAnimationWI||happy}} {now}" {
		t.Errorf("block rescanned: %q", got)
	}
}

func TestCommandsBlockBudget(t *testing.T) {
	for _, brain := range Brains {
		c := Commands(brain, SampleEmotions)
		if n := Estimate(c); n > 170 {
			t.Errorf("%s: commands block is %d tokens, cap 170", brain, n)
		}
		if strings.Contains(c, "newVoiceRequest") {
			t.Errorf("%s: commands block offers newVoiceRequest (F5: forkFollowUp listens again)", brain)
		}
		if strings.Contains(c, "lookingUp") {
			t.Errorf("%s: getImage offers lookingUp", brain)
		}
	}
}

// OllamaEt's own blocks are Estonian; every other brain keeps the English ones.
func TestEstonianBlocksOnlyOllamaEt(t *testing.T) {
	for _, brain := range Brains {
		v := Sample(brain, lang.Estonian)
		et := strings.HasPrefix(v.Speech, "Kasutaja") && strings.HasPrefix(v.Commands, "Tegutse") && strings.HasPrefix(v.Lang, "Vasta")
		en := strings.HasPrefix(v.Speech, "The user") && strings.HasPrefix(v.Commands, "Act by") && strings.HasPrefix(v.Lang, "Answer in")
		if brain == OllamaEt && !et || brain != OllamaEt && !en {
			t.Errorf("%s: speech %q, commands %q, lang %q", brain, v.Speech, v.Commands, v.Lang)
		}
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
	if ReplyCap(Claude) != 1024 || ReplyCap(Ollama) != 300 {
		t.Errorf("ReplyCap claude %d ollama %d", ReplyCap(Claude), ReplyCap(Ollama))
	}
	if LongReplyCap(Claude) != 2048 || LongReplyCap(Ollama) != 1000 {
		t.Errorf("LongReplyCap claude %d ollama %d", LongReplyCap(Claude), LongReplyCap(Ollama))
	}
}
