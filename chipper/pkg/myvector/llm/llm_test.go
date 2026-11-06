package llm

import (
	"os"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// tempPath points lang.Path at a fresh settings file.
func tempPath(t *testing.T) string {
	t.Helper()
	lang.UseForTest(t, lang.English)
	path := lang.Path()
	os.Remove(path)
	return path
}

func TestLLMETModel(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{``, DefaultETModel}, // no file
		{`{"lang":"et-EE"}`, DefaultETModel},
		{`{"et_model":""}`, DefaultETModel},
		{`{"et_model":7}`, DefaultETModel},
		{`not json`, DefaultETModel},
		{`{"et_model":"estllm:test","lang":"et-EE"}`, "estllm:test"},
	} {
		path := tempPath(t)
		if c.file != "" {
			os.WriteFile(path, []byte(c.file), 0644)
		}
		if got := ETModel(); got != c.want {
			t.Errorf("%s: ETModel() = %q, want %q", c.file, got, c.want)
		}
	}
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"et_model":"estllm:kept"}`), 0644)
	if err := lang.Save(lang.Estonian); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != "estllm:kept" {
		t.Fatalf("ETModel() after lang.Save = %q", got)
	}
}

func TestLLMSaveETModel(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"lang":"et-EE","t1":"cori"}`), 0644)
	if err := SaveETModel("  estllm:new  "); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != "estllm:new" {
		t.Fatalf("ETModel() after SaveETModel = %q, want the trimmed value", got)
	}
	b, _ := os.ReadFile(path)
	if want := "{\n  \"et_model\": \"estllm:new\",\n  \"lang\": \"et-EE\",\n  \"t1\": \"cori\"\n}\n"; string(b) != want {
		t.Fatalf("file = %q, want %q", b, want)
	}
	for _, bad := range []string{"estllm: new", "est\tllm", strings.Repeat("m", ModelMax+1)} {
		if err := SaveETModel(bad); err == nil {
			t.Errorf("SaveETModel(%.20q) accepted", bad)
		}
	}
	if got := ETModel(); got != "estllm:new" {
		t.Fatalf("a rejected SaveETModel changed ETModel to %q", got)
	}
	if err := SaveETModel(strings.Repeat("é", ModelMax)); err != nil { // the limit is characters, not bytes
		t.Fatalf("%d characters: %v", ModelMax, err)
	}
	if err := SaveETModel(" "); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != DefaultETModel {
		t.Fatalf("ETModel() after an empty SaveETModel = %q, want the default", got)
	}
}

const ollamaV1 = "http://localhost:11434/v1"

func TestLLMResolveLegacy(t *testing.T) {
	ollama := KG{"custom", ollamaV1, "qwen3.5:4b"}
	for _, c := range []struct {
		name string
		l    string
		kg   KG
		want Target
		ok   bool
	}{
		{"en ollama", lang.English, ollama, Target{}, false},
		{"et ollama", lang.Estonian, ollama, Target{Ollama, ollamaV1, "ollama", "estllm:x"}, true},
		{"et openai", lang.Estonian, KG{"openai", "", "gpt-4o-mini"}, Target{}, false},
		{"et claude preset", lang.Estonian, KG{"custom", "https://api.anthropic.com/v1", "claude-haiku-5-5"}, Target{}, false},
	} {
		got, ok := Resolve(Config{LegacyETModel: "estllm:x"}, c.l, c.kg)
		if got != c.want || ok != c.ok {
			t.Errorf("%s: Resolve = %+v %v, want %+v %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestLLMEffectiveDefaults(t *testing.T) {
	p, u := Effective(Config{}, KG{"custom", ollamaV1 + "/", "gemma4:e4b"})
	if p[lang.English] != (Profile{Ollama, "gemma4:e4b"}) || p[lang.Estonian] != (Profile{Ollama, DefaultETModel}) || u != "http://localhost:11434" {
		t.Errorf("Ollama Knowledge: %+v %q", p, u)
	}
	p, u = Effective(Config{LegacyETModel: "estllm:x"}, KG{"openai", "", "gpt-4o-mini"})
	if p[lang.English] != (Profile{Ollama, DefaultENModel}) || p[lang.Estonian] != (Profile{Ollama, "estllm:x"}) || u != DefaultOllamaURL {
		t.Errorf("OpenAI Knowledge: %+v %q", p, u)
	}
	saved := Config{Profiles: map[string]Profile{lang.Estonian: {Claude, "claude-haiku-5-5"}, "xx": {Ollama, "junk"}}, OllamaURL: "http://pc:11434"}
	p, u = Effective(saved, KG{"custom", ollamaV1, "gemma4:e4b"})
	if p[lang.Estonian] != (Profile{Claude, "claude-haiku-5-5"}) || p[lang.English] != (Profile{Ollama, "gemma4:e4b"}) || len(p) != 2 || u != "http://pc:11434" {
		t.Errorf("saved: %+v %q", p, u)
	}
}

func TestLLMLoad(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"llm":{"et-EE":{"provider":"claude","model":"m"}},"ollama_url":"http://x:1","claude_key":"k","et_model":"e"}`), 0644)
	c := Load()
	if c.Profiles[lang.Estonian] != (Profile{Claude, "m"}) || c.OllamaURL != "http://x:1" || c.ClaudeKey != "k" || c.LegacyETModel != "e" {
		t.Errorf("Load = %+v", c)
	}
	os.WriteFile(path, []byte(`{"lang":"en-US"}`), 0644)
	if Load().Profiles != nil {
		t.Error("no llm key: Profiles not nil")
	}
}
