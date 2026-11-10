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

func TestResolveVision(t *testing.T) {
	old := AnthropicBase
	AnthropicBase = "https://claude.test"
	t.Cleanup(func() { AnthropicBase = old })
	const key, pc = "sk-ant-0123456789abcdefWxYz", "http://pc:11434"
	legacy, kg := Config{LegacyETModel: "estllm:x"}, KG{"custom", ollamaV1, "llama:k"}
	claudeBrain := map[string]Profile{lang.English: {Claude, "claude-haiku-5-5"}, lang.Estonian: {Claude, "claude-haiku-5-5"}}
	saved := func(vision map[string]Profile, key string) Config {
		return Config{OllamaURL: pc, ClaudeKey: key, Profiles: claudeBrain, Vision: vision}
	}
	claude := Target{Claude, "https://claude.test/v1", key, "claude-sonnet-5-5"}
	for _, w := range []struct {
		name string
		c    Config
		l    string
		kg   KG
		want Target
		ok   bool
	}{
		{"legacy openai", legacy, lang.Estonian, KG{"openai", "", "gpt-4o-mini"}, Target{}, false},
		{"legacy en openai", legacy, lang.English, KG{"openai", "", "gpt-4o-mini"}, Target{}, false},
		{"legacy et ollama", legacy, lang.Estonian, kg, Target{Ollama, ollamaV1, "ollama", DefaultVisionET}, true},
		{"legacy en ollama", legacy, lang.English, kg, Target{Ollama, ollamaV1, "ollama", DefaultVisionEN}, true},
		{"brain claude, unsaved: et", saved(nil, key), lang.Estonian, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionET}, true},
		{"brain claude, unsaved: en", saved(nil, key), lang.English, KG{}, Target{Ollama, pc + "/v1", "ollama", DefaultVisionEN}, true},
		{"saved ollama", saved(map[string]Profile{lang.Estonian: {Ollama, "llava:7b"}}, ""), lang.Estonian, kg, Target{Ollama, pc + "/v1", "ollama", "llava:7b"}, true},
		{"saved ollama, other language default", saved(map[string]Profile{lang.Estonian: {Ollama, "llava:7b"}}, ""), lang.English, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionEN}, true},
		{"hand-edited gemma4", saved(map[string]Profile{lang.Estonian: {Ollama, "gemma4:e4b-it-qat"}}, ""), lang.Estonian, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionET}, true},
		{"saved claude + key", saved(map[string]Profile{lang.English: {Claude, "claude-sonnet-5-5"}}, key), lang.English, kg, claude, true},
		{"saved claude, no key", saved(map[string]Profile{lang.English: {Claude, "claude-sonnet-5-5"}}, ""), lang.English, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionEN}, true},
		{"vision only saved", Config{Vision: map[string]Profile{lang.English: {Ollama, "v:1"}}}, lang.English, KG{"openai", "", ""}, Target{Ollama, DefaultOllamaURL + "/v1", "ollama", "v:1"}, true},
		{"unknown language", saved(nil, key), "fr-FR", kg, Target{}, false},
		{"hand-edited empty model", saved(map[string]Profile{lang.English: {Ollama, ""}}, key), lang.English, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionEN}, true},
		{"hand-edited provider", saved(map[string]Profile{lang.English: {"openai", "gpt-4o"}}, key), lang.English, kg, Target{Ollama, pc + "/v1", "ollama", DefaultVisionEN}, true},
	} {
		if got, ok := ResolveVision(w.c, w.l, w.kg); got != w.want || ok != w.ok {
			t.Errorf("%s: ResolveVision = %+v %v, want %+v %v", w.name, got, ok, w.want, w.ok)
		}
	}
}

func TestValidateVision(t *testing.T) {
	key := "sk-ant-0123456789abcdefWxYz"
	for name, c := range map[string]Config{
		"provider":      {Vision: map[string]Profile{lang.English: {"gemini", "g"}}},
		"claude no key": {Vision: map[string]Profile{lang.Estonian: {Claude, "claude-haiku-5-5"}}},
		"language":      {Vision: map[string]Profile{"fr-FR": {Ollama, "m"}}},
		"empty model":   {Vision: map[string]Profile{lang.English: {Ollama, ""}}},
		"gemma4":        {Vision: map[string]Profile{lang.Estonian: {Ollama, "gemma4:e4b-it-qat"}}},
		"padded gemma4": {Vision: map[string]Profile{lang.Estonian: {Ollama, " gemma4:e4b "}}},
	} {
		if err := Validate(c); err == nil || !strings.HasPrefix(err.Error(), "vision ") {
			t.Errorf("%s: Validate(%+v) = %v, want a vision error", name, c, err)
		}
	}
	path := tempPath(t)
	brain := map[string]Profile{lang.Estonian: {Claude, "claude-haiku-5-5"}}
	if err := Save(Config{ClaudeKey: key, Profiles: brain, Vision: map[string]Profile{lang.Estonian: {Ollama, " llava:7b "}}}); err != nil {
		t.Fatal(err)
	}
	if c := Load(); c.Profiles[lang.Estonian] != brain[lang.Estonian] || c.Vision[lang.Estonian] != (Profile{Ollama, "llava:7b"}) {
		t.Errorf("saved %+v", c)
	}
	if err := Save(Config{ClaudeKey: key, Profiles: brain}); err != nil { // vision absent: kept
		t.Fatal(err)
	}
	if c := Load(); c.Vision[lang.Estonian] != (Profile{Ollama, "llava:7b"}) || c.Profiles[lang.Estonian] != brain[lang.Estonian] {
		b, _ := os.ReadFile(path)
		t.Errorf("vision-less Save: %+v (%s)", c, b)
	}
}

func TestVisionAllowed(t *testing.T) {
	for m, want := range map[string]bool{"qwen3.5:4b": true, "llava:7b": true, "library/llava:7b": true, "mygemma4:x": true,
		"gemma4:e4b": false, "library/gemma4:e4b": false, "Gemma4:x": false, "GEMMA4": false, " gemma4:e4b ": false} {
		if got := VisionAllowed(m); got != want {
			t.Errorf("VisionAllowed(%q) = %v, want %v", m, got, want)
		}
	}
}

func TestLoadDropsNoVision(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"vision":{"et-EE":{"provider":"ollama","model":"gemma4:e4b"},"en-US":{"provider":"ollama","model":"llava:7b"}}}`), 0644)
	if c := Load(); len(c.Vision) != 1 || c.Vision[lang.English] != (Profile{Ollama, "llava:7b"}) {
		t.Errorf("Load vision = %+v, want only en-US llava:7b", c.Vision)
	}
}
