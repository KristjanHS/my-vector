package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// useEpisodes points Path at a temp dir and the config at cfg; returns the episodes root.
func useEpisodes(t *testing.T, cfg string) string {
	t.Helper()
	oldPath, oldCfg := Path, configBytes
	dir := t.TempDir()
	Path = func() string { return filepath.Join(dir, "myvectorMemory.json") }
	configBytes = func() ([]byte, error) { return []byte(cfg), nil }
	t.Cleanup(func() { Path, configBytes = oldPath, oldCfg })
	return filepath.Join(dir, "myvectorMemory")
}

func readEpisodes(t *testing.T, path string) []Episode {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Episode
	for sc := bufio.NewScanner(f); sc.Scan(); {
		var e Episode
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func TestCaptureAppend(t *testing.T) {
	root := useEpisodes(t, `{}`)
	la, _ := time.LoadLocation("America/Los_Angeles")
	at := time.Date(2026, 11, 7, 7, 43, 0, 0, time.UTC) // 2026-11-06 23:43 in LA
	chat := Episode{At: at, Kind: Chat, User: "kas mu koer Muki on terve?", Reply: "Jah.", Lang: "et-EE"}
	intent := Episode{At: at.Add(time.Minute), Kind: Intent, Intent: "intent_clock_time", User: "mis kell on", Lang: "et-EE"}
	for _, e := range []Episode{chat, intent} {
		if err := Capture("00e20100", e, la); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "00e20100", "2026-11-06.jsonl")
	got := readEpisodes(t, path)
	if len(got) != 2 || !got[0].At.Equal(at) || got[0].User != chat.User || got[0].Reply != chat.Reply || got[1].Intent != intent.Intent || got[1].Kind != Intent {
		t.Errorf("episodes = %+v", got)
	}
	for p, want := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: mode %v, want %v (%v)", p, fi.Mode().Perm(), want, err)
		}
	}
}

func TestCaptureOffAndBadESN(t *testing.T) {
	root := useEpisodes(t, `{"memory_model": "off"}`)
	e := Episode{At: time.Now(), Kind: Chat, User: "tere"}
	if err := Capture("00e20100", e, time.UTC); err != nil {
		t.Fatal(err)
	}
	configBytes = func() ([]byte, error) { return []byte(`{}`), nil }
	for _, bad := range []string{"", ".", "..", "../x", `a\b`} {
		if err := Capture(bad, e, time.UTC); err == nil {
			t.Errorf("Capture(%q) = nil, want an error", bad)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("off / bad esn wrote %s: %v", root, err)
	}
}

func TestModel(t *testing.T) {
	for cfg, want := range map[string]string{
		``:                               "ollama:gemma4:e4b-it-qat",
		`{bad`:                           "ollama:gemma4:e4b-it-qat",
		`{"memory_model": ""}`:           "ollama:gemma4:e4b-it-qat",
		`{"memory_model": " claude:x "}`: "claude:x",
		`{"memory_model": "off"}`:        "off",
	} {
		useEpisodes(t, cfg)
		if got := Model(); got != want {
			t.Errorf("Model(%s) = %q, want %q", cfg, got, want)
		}
		if Off() != (want == "off") {
			t.Errorf("Off(%s) = %v", cfg, Off())
		}
	}
}

func TestTakeLLM(t *testing.T) {
	old := Now
	t.Cleanup(func() { Now = old })
	now := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	Now = func() time.Time { return now }
	MarkLLM("a", "tere")
	if TakeLLM("b", "tere") || TakeLLM("a", "tere!") {
		t.Error("TakeLLM matched another esn / text")
	}
	if !TakeLLM("a", "tere") || TakeLLM("a", "tere") {
		t.Error("TakeLLM: want true once, then consumed")
	}
	MarkLLM("a", "tere")
	now = now.Add(pendingTTL - time.Second)
	if !TakeLLM("a", "tere") {
		t.Error("TakeLLM inside pendingTTL = false")
	}
	MarkLLM("a", "tere")
	now = now.Add(pendingTTL)
	if TakeLLM("a", "tere") {
		t.Error("TakeLLM after pendingTTL = true")
	}
	if pendingTTL != time.Minute {
		t.Errorf("pendingTTL = %v", pendingTTL)
	}
}

func TestTouch(t *testing.T) {
	old := Now
	t.Cleanup(func() { Now = old })
	if LastTurn().IsZero() {
		t.Error("LastTurn not initialised to process start")
	}
	at := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	Now = func() time.Time { return at }
	Touch()
	if !LastTurn().Equal(at) {
		t.Errorf("LastTurn = %v, want %v", LastTurn(), at)
	}
}
