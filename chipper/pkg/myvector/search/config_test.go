package search

import (
	"path/filepath"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// A bad file reads as defaults; a type error in one key keeps the keys that did decode.
func TestLoadConfigBadFile(t *testing.T) {
	def := Config{SearxngURL: DefaultSearxngURL}
	for _, c := range []struct {
		body string
		want Config
	}{
		{``, def},
		{`{`, def},
		{`{"search":{"provider":5,"tavily_key":"T"}}`, Config{TavilyKey: "T", SearxngURL: DefaultSearxngURL}},
		{`{"search":{"provider":"tavily","tavily_key":"T"}}`, Config{Provider: "tavily", TavilyKey: "T", SearxngURL: DefaultSearxngURL}},
	} {
		useConfig(t, c.body)
		if got := LoadConfig(); got != c.want {
			t.Errorf("%q: LoadConfig = %+v, want %+v", c.body, got, c.want)
		}
	}
	old := lang.Path
	lang.Path = func() string { return filepath.Join(t.TempDir(), "none.json") }
	t.Cleanup(func() { lang.Path = old })
	if got := LoadConfig(); got != def {
		t.Errorf("missing: LoadConfig = %+v", got)
	}
}
