// Package llm picks each language's brain — an Ollama or Claude model — saved in myvectorConfig.json beside "lang".
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

const (
	Ollama = "ollama"
	Claude = "claude"
	// DefaultETModel is the et-EE Ollama model (Estonian plan E11, E25) when nothing else is saved.
	DefaultETModel   = "hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M"
	DefaultENModel   = "qwen3.5:4b"
	DefaultOllamaURL = "http://localhost:11434"
	AnthropicVersion = "2023-06-01"
	// ModelMax is the longest model name ValidModel takes, in characters.
	ModelMax = 200
)

// AnthropicBase is Claude's API root; tests point it at an httptest server.
var AnthropicBase = "https://api.anthropic.com"

// OllamaPort marks a "custom" Knowledge endpoint as Ollama (Estonian plan E27); tests point it at an httptest port.
var OllamaPort = "11434"

type Profile struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Config is the file's LLM keys; Profiles == nil means the page never saved them (legacy: wire-pod Knowledge + et_model).
type Config struct {
	Profiles      map[string]Profile
	OllamaURL     string
	ClaudeKey     string
	LegacyETModel string
}

// KG is the upstream Knowledge fields the resolver reads (llm must not import vars).
type KG struct{ Provider, Endpoint, Model string }

// Target is where one request goes.
type Target struct{ Provider, BaseURL, Key, Model string }

func (t Target) Ollama() bool { return t.Provider == Ollama }

// IsOllama is a "custom" Knowledge endpoint on OllamaPort.
func IsOllama(kg KG) bool {
	if kg.Provider != "custom" {
		return false
	}
	u, err := url.Parse(kg.Endpoint)
	return err == nil && u.Port() == OllamaPort
}

// Load reads lang.Path() on every call (once per question, so hand edits apply); a missing or bad file is legacy.
func Load() Config {
	var f struct {
		LLM       map[string]Profile `json:"llm"`
		OllamaURL string             `json:"ollama_url"`
		ClaudeKey string             `json:"claude_key"`
		ETModel   any                `json:"et_model"`
	}
	b, err := os.ReadFile(lang.Path())
	if err != nil || json.Unmarshal(b, &f) != nil {
		return Config{}
	}
	c := Config{Profiles: f.LLM, OllamaURL: f.OllamaURL, ClaudeKey: f.ClaudeKey}
	if m, ok := f.ETModel.(string); ok {
		c.LegacyETModel = m
	}
	return c
}

// ETModel is the saved "et_model"; missing, empty or not a string gives DefaultETModel.
func ETModel() string { return Load().etModel() }

func (c Config) etModel() string {
	if c.LegacyETModel != "" {
		return c.LegacyETModel
	}
	return DefaultETModel
}

// ValidModel: after trimming, no whitespace and at most ModelMax characters (empty is fine).
func ValidModel(m string) error {
	m = strings.TrimSpace(m)
	if strings.IndexFunc(m, unicode.IsSpace) >= 0 {
		return errors.New("model: no whitespace")
	}
	if len([]rune(m)) > ModelMax {
		return fmt.Errorf("model: at most %d characters", ModelMax)
	}
	return nil
}

// SaveETModel validates and trims m and writes it as "et_model"; empty gives DefaultETModel.
func SaveETModel(m string) error {
	if err := ValidModel(m); err != nil {
		return err
	}
	return conf.Update(lang.Path(), map[string]any{"et_model": strings.TrimSpace(m)})
}

// Effective is what the page shows: each language's saved profile or its default, and the Ollama URL.
func Effective(c Config, kg KG) (map[string]Profile, string) {
	p := map[string]Profile{
		lang.English:  {Ollama, DefaultENModel},
		lang.Estonian: {Ollama, c.etModel()},
	}
	if IsOllama(kg) && kg.Model != "" {
		p[lang.English] = Profile{Ollama, kg.Model}
	}
	for l, s := range c.Profiles {
		if _, ok := p[l]; ok {
			p[l] = s
		}
	}
	u := c.OllamaURL
	if u == "" && IsOllama(kg) {
		u = strings.TrimSuffix(strings.TrimRight(kg.Endpoint, "/"), "/v1")
	}
	if u == "" {
		u = DefaultOllamaURL
	}
	return p, u
}

// Resolve is language l's Target; false leaves the request on wire-pod's Knowledge as-is.
// Legacy (nothing saved): en-US → false; et-EE → the ET model on Knowledge's endpoint, only when that is Ollama.
func Resolve(c Config, l string, kg KG) (Target, bool) {
	if l != lang.Estonian || !IsOllama(kg) {
		return Target{}, false
	}
	return Target{Ollama, kg.Endpoint, "ollama", c.etModel()}, true
}
