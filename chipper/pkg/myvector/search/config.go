package search

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

const DefaultSearxngURL = "http://127.0.0.1:8888"

// DefaultOrder is the W8 fallback order; Stage 3.3 (row B1) rewrites it from the bench verdict.
var DefaultOrder = []string{"tavily", "ollama", "searxng"}

// Config is "search" in myvectorConfig.json (W12).
type Config struct {
	Provider     string `json:"provider"` // auto (or ""), off, tavily, ollama, searxng
	TavilyKey    string `json:"tavily_key"`
	OllamaKey    string `json:"ollama_key"`
	SearxngURL   string `json:"searxng_url"`
	WeatherPlace string `json:"weather_place"`
}

// LoadConfig reads lang.Path() on every call; a missing or bad file is the zero Config (SearxngURL defaulted).
func LoadConfig() Config {
	var f struct {
		Search Config `json:"search"`
	}
	if b, err := os.ReadFile(lang.Path()); err == nil {
		json.Unmarshal(b, &f)
	}
	c := f.Search
	if strings.TrimSpace(c.SearxngURL) == "" {
		c.SearxngURL = DefaultSearxngURL
	}
	return c
}

// ProviderFor is the named provider when it has its key / URL.
func (c Config) ProviderFor(name string) (Provider, bool) {
	switch name {
	case "tavily":
		return Tavily{c.TavilyKey}, c.TavilyKey != ""
	case "ollama":
		return Ollama{c.OllamaKey}, c.OllamaKey != ""
	case "searxng":
		return Searxng{c.SearxngURL}, c.SearxngURL != ""
	}
	return nil, false
}
