package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

const DefaultSearxngURL = "http://127.0.0.1:8888"

// DefaultOrder is the W8 fallback order (row B1); ollama is left out: its web search is 402 on the free tier.
var DefaultOrder = []string{"tavily", "searxng"}

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

// Providers is the W12 provider select.
var Providers = []string{"auto", "off", "tavily", "ollama", "searxng"}

// Validate checks a Config for Save.
func Validate(c Config) error {
	if c.Provider != "" && !slices.Contains(Providers, c.Provider) {
		return fmt.Errorf("search.provider: one of %v", Providers)
	}
	for name, k := range map[string]string{"tavily_key": c.TavilyKey, "ollama_key": c.OllamaKey} {
		if len(k) > 200 || strings.IndexFunc(k, func(r rune) bool { return r <= ' ' || r > '~' }) >= 0 {
			return fmt.Errorf("search.%s: printable ASCII, no whitespace, at most 200 characters", name)
		}
	}
	if u, err := url.Parse(c.SearxngURL); c.SearxngURL != "" && (err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
		return errors.New("search.searxng_url: an http(s) URL")
	}
	if utf8.RuneCountInString(c.WeatherPlace) > 100 || strings.IndexFunc(c.WeatherPlace, unicode.IsControl) >= 0 {
		return errors.New("search.weather_place: at most 100 characters, one line")
	}
	return nil
}

// Save validates c and writes it as "search", keys and URL trimmed.
func Save(c Config) error {
	c.TavilyKey, c.OllamaKey = strings.TrimSpace(c.TavilyKey), strings.TrimSpace(c.OllamaKey)
	c.SearxngURL, c.WeatherPlace = strings.TrimRight(strings.TrimSpace(c.SearxngURL), "/"), strings.TrimSpace(c.WeatherPlace)
	if err := Validate(c); err != nil {
		return err
	}
	return conf.Update(lang.Path(), map[string]any{"search": c})
}

// Use is one provider's calls this UTC month against its cap.
type Use struct {
	Provider string `json:"provider"`
	Used     int    `json:"used"`
	Cap      int    `json:"cap"`
}

// Usage lists every capped provider's Use at now.
func Usage(now time.Time) []Use {
	st := current()
	var us []Use
	for _, p := range []string{"tavily", "ollama", "searxng", "open-meteo"} {
		us = append(us, Use{p, st.UsedThisMonth(p, now), Caps[p]})
	}
	return us
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
