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

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/sashabaranov/go-openai"
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
	if c.Profiles == nil {
		if l != lang.Estonian || !IsOllama(kg) {
			return Target{}, false
		}
		return Target{Ollama, kg.Endpoint, "ollama", c.etModel()}, true
	}
	ps, u := Effective(c, kg)
	p, ok := ps[l]
	switch {
	case !ok || p.Model == "":
		return Target{}, false
	case p.Provider == Ollama:
		return Target{Ollama, u + "/v1", "ollama", p.Model}, true
	case p.Provider == Claude && c.ClaudeKey == "":
		logger.Println("myvector llm: " + l + " claude profile has no key — using wire-pod Knowledge")
		return Target{}, false
	case p.Provider == Claude:
		return Target{Claude, AnthropicBase + "/v1", c.ClaudeKey, p.Model}, true
	}
	return Target{}, false
}

// VisionModel is the Ollama model an et-EE photo question uses (E26): the en-US one when that is Ollama.
func VisionModel(c Config, kg KG) string {
	if p, _ := Effective(c, kg); p[lang.English].Provider == Ollama && p[lang.English].Model != "" {
		return p[lang.English].Model
	}
	return DefaultENModel
}

// OllamaBase is t's Ollama root, for its native /api routes.
func (t Target) OllamaBase() string {
	return strings.TrimSuffix(strings.TrimRight(t.BaseURL, "/"), "/v1")
}

// NewClient is an OpenAI-compatible client for t (Claude takes the key as a Bearer token).
func NewClient(t Target) *openai.Client {
	cfg := openai.DefaultConfig(t.Key)
	cfg.BaseURL = t.BaseURL
	return openai.NewClientWithConfig(cfg)
}

// Validate checks a Config for Save; Profiles must hold only supported languages.
func Validate(c Config) error {
	for l, p := range c.Profiles {
		if !lang.Valid(l) {
			return fmt.Errorf("llm: language %q: one of %v", l, lang.Supported)
		}
		if p.Provider != Ollama && p.Provider != Claude {
			return fmt.Errorf("llm %s: provider %q: ollama or claude", l, p.Provider)
		}
		if strings.TrimSpace(p.Model) == "" {
			return fmt.Errorf("llm %s: model is empty", l)
		}
		if err := ValidModel(p.Model); err != nil {
			return fmt.Errorf("llm %s: %w", l, err)
		}
		if p.Provider == Claude && strings.TrimSpace(c.ClaudeKey) == "" {
			return fmt.Errorf("llm %s: a Claude model needs a Claude key", l)
		}
	}
	if err := ValidKey(c.ClaudeKey); err != nil {
		return err
	}
	return ValidOllamaURL(c.OllamaURL)
}

// ValidKey: empty (unset) or, trimmed, 20–200 printable ASCII characters without whitespace.
func ValidKey(k string) error {
	k = strings.TrimSpace(k)
	if k == "" {
		return nil
	}
	if len(k) < 20 || len(k) > 200 {
		return errors.New("claude_key: 20 to 200 characters")
	}
	for _, r := range k {
		if r <= ' ' || r > '~' {
			return errors.New("claude_key: printable ASCII, no whitespace")
		}
	}
	return nil
}

// ValidOllamaURL: empty (default) or an http(s) URL with a host, at most 200 characters.
func ValidOllamaURL(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > 200 {
		return errors.New("ollama_url: an http(s) URL with a host, at most 200 characters")
	}
	return nil
}

// Save validates c and writes "llm", "ollama_url" and "claude_key" together (the URL without a trailing / or /v1).
func Save(c Config) error {
	if err := Validate(c); err != nil {
		return err
	}
	var ps map[string]Profile // nil Profiles stays legacy ("llm": null)
	if c.Profiles != nil {
		ps = map[string]Profile{}
	}
	for l, p := range c.Profiles {
		ps[l] = Profile{p.Provider, strings.TrimSpace(p.Model)}
	}
	u := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(c.OllamaURL), "/"), "/v1")
	defer clearCache()
	return conf.Update(lang.Path(), map[string]any{"llm": ps, "ollama_url": u, "claude_key": strings.TrimSpace(c.ClaudeKey)})
}
