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

func TestLLMResolveSaved(t *testing.T) {
	old := AnthropicBase
	AnthropicBase = "https://claude.test"
	t.Cleanup(func() { AnthropicBase = old })
	kg := KG{"custom", ollamaV1, "qwen3.5:4b"}
	c := Config{OllamaURL: "http://pc:11434", ClaudeKey: "sk-ant-0123456789abcdefWxYz", Profiles: map[string]Profile{
		lang.English: {Ollama, "gemma4:e4b"}, lang.Estonian: {Claude, "claude-haiku-5-5"}}}
	if got, ok := Resolve(c, lang.English, kg); !ok || got != (Target{Ollama, "http://pc:11434/v1", "ollama", "gemma4:e4b"}) {
		t.Errorf("en: %+v %v", got, ok)
	}
	if got, ok := Resolve(c, lang.Estonian, kg); !ok || got != (Target{Claude, "https://claude.test/v1", c.ClaudeKey, "claude-haiku-5-5"}) {
		t.Errorf("et: %+v %v", got, ok)
	}
	c.ClaudeKey = "" // a hand edit
	if _, ok := Resolve(c, lang.Estonian, kg); ok {
		t.Error("claude profile without a key resolved")
	}
	c.Profiles = map[string]Profile{} // saved, but neither language set: defaults on the Ollama URL
	if got, ok := Resolve(c, lang.Estonian, KG{}); !ok || got != (Target{Ollama, "http://pc:11434/v1", "ollama", DefaultETModel}) {
		t.Errorf("et default: %+v %v", got, ok)
	}
}

func TestLLMSaveRejects(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"lang":"en-US"}`), 0644)
	key := "sk-ant-0123456789abcdefWxYz"
	for name, c := range map[string]Config{
		"provider":       {Profiles: map[string]Profile{lang.English: {"gemini", "g"}}},
		"language":       {Profiles: map[string]Profile{"fr-FR": {Ollama, "m"}}},
		"empty model":    {Profiles: map[string]Profile{lang.English: {Ollama, " "}}},
		"model space":    {Profiles: map[string]Profile{lang.English: {Ollama, "a b"}}},
		"model too long": {Profiles: map[string]Profile{lang.English: {Ollama, strings.Repeat("m", ModelMax+1)}}},
		"claude no key":  {Profiles: map[string]Profile{lang.Estonian: {Claude, "claude-haiku-5-5"}}},
		"short key":      {ClaudeKey: "sk-short"},
		"key whitespace": {ClaudeKey: "sk-ant-0123456789 abcdefWxYz"},
		"url scheme":     {OllamaURL: "ftp://pc:11434"},
		"url host":       {OllamaURL: "http://"},
	} {
		if err := Save(c); err == nil {
			t.Errorf("%s: Save accepted %+v", name, c)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != `{"lang":"en-US"}` {
		t.Fatalf("a rejected Save wrote %s", b)
	}
	if err := Save(Config{OllamaURL: " http://pc:11434/v1/ ", ClaudeKey: key, Profiles: map[string]Profile{lang.Estonian: {Claude, " claude-haiku-5-5 "}}}); err != nil {
		t.Fatal(err)
	}
	if c := Load(); c.OllamaURL != "http://pc:11434" || c.ClaudeKey != key || c.Profiles[lang.Estonian] != (Profile{Claude, "claude-haiku-5-5"}) {
		t.Errorf("saved %+v", c)
	}
}

func TestLLMResolveOllama(t *testing.T) {
	kg := KG{"custom", "https://api.anthropic.com/v1", "claude-haiku-5-5"}
	c := Config{OllamaURL: "http://pc:11434", ClaudeKey: "k", LegacyETModel: "estllm:x", Profiles: map[string]Profile{
		lang.English: {Claude, "claude-haiku-5-5"}, lang.Estonian: {Ollama, "estllm:y"}}}
	for _, w := range []struct {
		c Config
		l string
	}{{c, lang.English}, {c, lang.Estonian}, {Config{LegacyETModel: "estllm:x"}, lang.English}} {
		got, ok := ResolveOllama(w.c, w.l, kg, "listen:m")
		if !ok || !got.Ollama() || got.Model != "listen:m" || got.Key != "ollama" {
			t.Errorf("%s: %+v %v, want Ollama listen:m", w.l, got, ok)
		}
	}
	if got, _ := ResolveOllama(c, lang.Estonian, kg, "listen:m"); got.BaseURL != "http://pc:11434/v1" {
		t.Errorf("et: %+v, want the saved Ollama URL", got)
	}
	if _, ok := ResolveOllama(c, "xx", kg, "listen:m"); ok {
		t.Error("unknown language resolved")
	}
}
