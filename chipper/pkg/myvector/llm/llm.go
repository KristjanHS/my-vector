// Package llm picks each language's brain — an Ollama or Claude model — saved in myvectorConfig.json beside "lang".
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
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
	// DefaultETModel is the et-EE Ollama model when nothing else is saved (user ruling 2026-11-08: same model as listen.Model).
	DefaultETModel   = "gemma4:e4b-it-qat"
	DefaultENModel   = DefaultETModel // en-US default too (user ruling 2026-11-09: qwen3.5 thinking unusable, gemma4 is the saved en model)
	DefaultOllamaURL = "http://localhost:11434"
	// DefaultVisionEN / DefaultVisionET: each language's local camera model when "vision" is unsaved (plan vision-setting D1).
	DefaultVisionEN = "qwen3.5:4b"
	DefaultVisionET = "qwen3.5:4b"
	// NoVision is the family the camera never uses (2026-11-10-vision-et-prompt-bench.md (docs/plans/) D2): gemma4 sees
	// less than qwen3.5 (en keywords 28–40 vs 42–53), writes broken Estonian and breaks {{command}} syntax.
	NoVision         = "gemma4"
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
	Vision        map[string]Profile // saved camera profiles; nil = unsaved (DefaultVision per language)
	OllamaURL     string
	ClaudeKey     string
	LegacyETModel string
	Think         map[Path]Level // saved "think" levels per path; see Config.Thinking
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
		Vision    map[string]Profile `json:"vision"`
		OllamaURL string             `json:"ollama_url"`
		ClaudeKey string             `json:"claude_key"`
		ETModel   any                `json:"et_model"`
		Think     any                `json:"think"` // any: a hand-edited non-object must not drop the profiles
	}
	b, err := os.ReadFile(lang.Path())
	if err != nil || json.Unmarshal(b, &f) != nil {
		return Config{}
	}
	c := Config{Profiles: f.LLM, Vision: f.Vision, OllamaURL: f.OllamaURL, ClaudeKey: f.ClaudeKey}
	for l, p := range c.Vision { // a pre-ban or hand-edited NoVision camera would fail every Save and show on the page
		if !VisionAllowed(p.Model) {
			delete(c.Vision, l)
		}
	}
	if m, ok := f.ETModel.(string); ok {
		c.LegacyETModel = m
	}
	think, _ := f.Think.(map[string]any)
	for p, v := range think {
		s, _ := v.(string)
		if l, ok := ParseLevel(s); ok {
			if c.Think == nil {
				c.Think = map[Path]Level{}
			}
			c.Think[Path(p)] = l
		}
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

// ResolveOllama is model on the saved Ollama URL for any language Resolve knows (active listening).
func ResolveOllama(c Config, l string, kg KG, model string) (Target, bool) {
	ps, u := Effective(c, kg)
	if _, ok := ps[l]; !ok {
		return Target{}, false
	}
	return Target{Ollama, u + "/v1", "ollama", model}, true
}

// DefaultVision is language l's local camera model.
func DefaultVision(l string) string {
	if l == lang.Estonian {
		return DefaultVisionET
	}
	return DefaultVisionEN
}

// EffectiveVision is what the page shows: each language's saved camera profile or its local default.
func EffectiveVision(c Config) map[string]Profile {
	p := map[string]Profile{}
	for _, l := range []string{lang.English, lang.Estonian} {
		p[l] = Profile{Ollama, DefaultVision(l)}
		if s, ok := c.Vision[l]; ok {
			p[l] = s
		}
	}
	return p
}

// ResolveVision is language l's camera Target: the saved "vision" profile (Claude needs the key, else the local
// default), else DefaultVision on the Effective Ollama URL; false = legacy (nothing saved, Knowledge not Ollama).
func ResolveVision(c Config, l string, kg KG) (Target, bool) {
	p, ok := EffectiveVision(c)[l]
	if !ok || (c.Profiles == nil && c.Vision == nil && !IsOllama(kg)) {
		return Target{}, false
	}
	if p.Model == "" || (p.Provider != Ollama && p.Provider != Claude) || !VisionAllowed(p.Model) { // a hand edit Load does not validate
		p = Profile{Ollama, DefaultVision(l)}
	}
	if p.Provider == Claude && c.ClaudeKey != "" {
		return Target{Claude, AnthropicBase + "/v1", c.ClaudeKey, p.Model}, true
	}
	if p.Provider == Claude {
		logger.Println("myvector llm: " + l + " claude vision has no key — using " + DefaultVision(l))
		p.Model = DefaultVision(l)
	}
	_, u := Effective(c, kg)
	return Target{Ollama, u + "/v1", "ollama", p.Model}, true
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

// VisionAllowed reports whether model may be a camera model: not the NoVision family, matched on the name's last
// path segment, any case or padding ("library/Gemma4:x").
func VisionAllowed(model string) bool {
	return !strings.HasPrefix(strings.ToLower(path.Base(strings.TrimSpace(model))), NoVision)
}

// Validate checks a Config for Save; Profiles must hold only supported languages.
func Validate(c Config) error {
	for kind, ps := range map[string]map[string]Profile{"llm": c.Profiles, "vision": c.Vision} {
		for l, p := range ps {
			if err := validProfile(c, l, p); err != nil {
				return fmt.Errorf("%s %w", kind, err)
			}
			if kind == "vision" && !VisionAllowed(p.Model) {
				return fmt.Errorf("vision %s: %s is not a camera model (%s is out)", l, p.Model, NoVision)
			}
		}
	}
	if err := ValidKey(c.ClaudeKey); err != nil {
		return err
	}
	return ValidOllamaURL(c.OllamaURL)
}

func validProfile(c Config, l string, p Profile) error {
	if !lang.Valid(l) {
		return fmt.Errorf("language %q: one of %v", l, lang.Supported)
	}
	if p.Provider != Ollama && p.Provider != Claude {
		return fmt.Errorf("%s: provider %q: ollama or claude", l, p.Provider)
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("%s: model is empty", l)
	}
	if err := ValidModel(p.Model); err != nil {
		return fmt.Errorf("%s: %w", l, err)
	}
	if p.Provider == Claude && strings.TrimSpace(c.ClaudeKey) == "" {
		return fmt.Errorf("%s: a Claude model needs a Claude key", l)
	}
	return nil
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

// Save validates c and writes "llm", "ollama_url" and "claude_key" together (the URL without a trailing / or /v1),
// and "vision" when c.Vision is not nil (nil keeps the saved one).
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
	kv := map[string]any{"llm": ps, "ollama_url": u, "claude_key": strings.TrimSpace(c.ClaudeKey)}
	if c.Vision != nil {
		vs := map[string]Profile{}
		for l, p := range c.Vision {
			vs[l] = Profile{p.Provider, strings.TrimSpace(p.Model)}
		}
		kv["vision"] = vs
	}
	defer clearCache()
	return conf.Update(lang.Path(), kv)
}
