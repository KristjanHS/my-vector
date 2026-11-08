package wirepod_ttr

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
	"github.com/sashabaranov/go-openai"
)

// useMemory: et-EE, memory_model model ("" = unset), store in a temp dir, SaveChat on, robot "esn" in LA
// (forkNow 07:43 UTC = 2026-11-06 23:43 there); returns the episodes root.
func useMemory(t *testing.T, model string) string {
	t.Helper()
	lang.UseForTest(t, lang.Estonian)
	if model != "" {
		if err := conf.Update(lang.Path(), map[string]any{"memory_model": model}); err != nil {
			t.Fatal(err)
		}
	}
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	vars.APIConfig.Knowledge.SaveChat = true
	useBotClock(t, "America/Los_Angeles")
	old := memory.Path
	dir := t.TempDir()
	memory.Path = func() string { return filepath.Join(dir, "myvectorMemory.json") }
	t.Cleanup(func() { memory.Path = old })
	return filepath.Join(dir, "myvectorMemory")
}

// episodes is every captured line under root, in file order.
func episodes(t *testing.T, root string) []memory.Episode {
	t.Helper()
	var out []memory.Episode
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for sc := bufio.NewScanner(f); sc.Scan(); {
			var e memory.Episode
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			out = append(out, e)
		}
		return nil
	})
	return out
}

func TestCapture(t *testing.T) {
	t.Run("chat and intent", func(t *testing.T) {
		root := useMemory(t, "")
		forkCaptureChat("kas mu koer Muki on terve?", "Muki on terve.", "esn")
		forkCaptureIntent("esn", "intent_clock_time", "mis kell on")
		got := episodes(t, root)
		if len(got) != 2 {
			t.Fatalf("%d lines, want 2: %+v", len(got), got)
		}
		c, i := got[0], got[1]
		if c.Kind != memory.Chat || c.User != "kas mu koer Muki on terve?" || c.Reply != "Muki on terve." || c.Lang != lang.Estonian || !c.At.Equal(forkNow()) {
			t.Errorf("chat line = %+v", c)
		}
		if i.Kind != memory.Intent || i.Intent != "intent_clock_time" || i.User != "mis kell on" {
			t.Errorf("intent line = %+v", i)
		}
		if _, err := os.Stat(filepath.Join(root, "esn", "2026-11-06.jsonl")); err != nil {
			t.Errorf("not in the robot-zone day file: %v", err)
		}
	})
	t.Run("upstream wiring", func(t *testing.T) {
		root := useMemory(t, "")
		old := vars.RememberedChats
		t.Cleanup(func() { vars.RememberedChats = old })
		Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "tere"},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "Tere!"}, "esn")
		IntentPass(&vtt.IntentGraphRequest{Device: "esn", Stream: &listenStream{}}, "intent_clock_time", "mis kell on", nil, false)
		if got := episodes(t, root); len(got) != 2 || got[0].Kind != memory.Chat || got[1].Kind != memory.Intent {
			t.Errorf("Remember + IntentPass captured %+v, want chat then intent", got)
		}
	})
	t.Run("intent_system_unmatched and empty text", func(t *testing.T) {
		root := useMemory(t, "")
		forkCaptureIntent("esn", "intent_system_unmatched", "blah blah")
		forkCaptureIntent("esn", "intent_clock_time", " ")
		if got := episodes(t, root); len(got) != 0 {
			t.Errorf("captured %+v", got)
		}
	})
	t.Run("LLM-path greeting", func(t *testing.T) {
		root := useMemory(t, "")
		CreateAIReq("räägi mulle Mukist", "esn", false, false) // forkAIReq MarkLLMs it
		forkCaptureIntent("esn", "intent_greeting_hello", "räägi mulle Mukist")
		if got := episodes(t, root); len(got) != 0 {
			t.Fatalf("LLM greeting captured: %+v", got)
		}
		forkCaptureIntent("esn", "intent_greeting_hello", "räägi mulle Mukist") // mark consumed
		if got := episodes(t, root); len(got) != 1 {
			t.Errorf("unmarked greeting: %d lines, want 1", len(got))
		}
	})
	t.Run("off", func(t *testing.T) {
		root := useMemory(t, "off")
		forkCaptureChat("tere", "Tere!", "esn")
		forkCaptureIntent("esn", "intent_clock_time", "mis kell on")
		if got := episodes(t, root); len(got) != 0 {
			t.Errorf("off captured %+v", got)
		}
	})
	t.Run("SaveChat false", func(t *testing.T) {
		root := useMemory(t, "")
		vars.APIConfig.Knowledge.SaveChat = false
		forkCaptureChat("tere", "Tere!", "esn")
		forkCaptureIntent("esn", "intent_clock_time", "mis kell on")
		if got := episodes(t, root); len(got) != 1 || got[0].Kind != memory.Intent {
			t.Errorf("SaveChat false: %+v, want the intent line only", got)
		}
	})
}

// TestCaptureTouch: chat, intent (even a skipped one) and forkAIReq each stamp memory.LastTurn.
func TestCaptureTouch(t *testing.T) {
	useMemory(t, "")
	old := memory.Now
	t.Cleanup(func() { memory.Now = old })
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	for name, turn := range map[string]func(){
		"chat":   func() { forkCaptureChat("tere", "Tere!", "esn") },
		"intent": func() { forkCaptureIntent("esn", "intent_system_unmatched", "blah") },
		"aireq":  func() { CreateAIReq("tere", "esn", false, true) },
	} {
		at = at.Add(time.Minute)
		stamp := at
		memory.Now = func() time.Time { return stamp }
		turn()
		if !memory.LastTurn().Equal(stamp) {
			t.Errorf("%s: LastTurn = %v, want %v", name, memory.LastTurn(), stamp)
		}
	}
}

// TestMemoryPathStubbed: TestMain keeps every IntentPass / Remember in other tests off the real chipper/.
func TestMemoryPathStubbed(t *testing.T) {
	if p := memory.Path(); !strings.HasPrefix(p, os.TempDir()) {
		t.Fatalf("memory.Path() = %q: ttr tests would write episodes under chipper/", p)
	}
}

func TestMemoryLoopNotInTests(t *testing.T) {
	if forkStartMemory(os.Args[0]) {
		t.Fatalf("memory loop started in test binary %q", os.Args[0])
	}
}
