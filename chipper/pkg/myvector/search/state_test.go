package search

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestQuota(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorSearch.json")
	nov := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	s := Open(path)
	for i := 1; i < CapTavily; i++ {
		if !s.Take("tavily", nov) {
			t.Fatalf("Take %d = false", i)
		}
	}
	s = Open(path) // counts survive reload
	if got := s.UsedThisMonth("tavily", nov); got != CapTavily-1 {
		t.Fatalf("reloaded used = %d, want %d", got, CapTavily-1)
	}
	if !s.Take("tavily", nov) {
		t.Fatalf("Take %d = false, want true", CapTavily)
	}
	if s.Take("tavily", nov) {
		t.Fatalf("Take %d = true, want false (capped)", CapTavily+1)
	}
	if !s.Take("searxng", nov) {
		t.Fatal("searxng capped by tavily's count")
	}
	dec := nov.Add(2 * time.Hour) // 2026-12-01 01:00 UTC
	if !s.Take("tavily", dec) {
		t.Fatal("Take in the next month = false")
	}
	if got := Open(path).UsedThisMonth("tavily", dec); got != 1 {
		t.Fatalf("December used = %d, want 1", got)
	}
}

func TestCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorSearch.json")
	t0 := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := Open(path)
	s.Put(Live, "Mis on euro kurss?", "live block", t0)
	s.Put(Recent, "kes on Eesti peaminister", "recent block", t0)
	s = Open(path)
	if b, ok := s.Get(Live, "mis on   euro kurss", t0.Add(29*time.Minute)); !ok || b != "live block" {
		t.Fatalf("Live at 29 min = %q %v, want hit (normalised sentence)", b, ok)
	}
	if _, ok := s.Get(Live, "Mis on euro kurss?", t0.Add(31*time.Minute)); ok {
		t.Fatal("Live at 31 min = hit, want miss")
	}
	if _, ok := s.Get(Recent, "kes on Eesti peaminister", t0.Add(5*time.Hour+59*time.Minute)); !ok {
		t.Fatal("Recent at 5 h 59 min = miss, want hit")
	}
	if _, ok := s.Get(Weather, "Mis on euro kurss?", t0); ok {
		t.Fatal("another class shares the entry")
	}

	s = Open(filepath.Join(t.TempDir(), "evict.json"))
	for i := 0; i <= CacheMax; i++ {
		s.Put(Live, fmt.Sprint("q", i), "b", t0.Add(time.Duration(i)*time.Second))
	}
	if len(s.Cache) != CacheMax {
		t.Fatalf("cache size %d, want %d", len(s.Cache), CacheMax)
	}
	if _, ok := s.Get(Live, "q0", t0.Add(CacheMax*time.Second)); ok {
		t.Fatal("oldest entry survived the 201st put")
	}
	if _, ok := s.Get(Live, "q1", t0.Add(CacheMax*time.Second)); !ok {
		t.Fatal("second-oldest evicted")
	}
}

func TestStateCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorSearch.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	s := Open(path)
	if !s.Take("searxng", time.Now()) {
		t.Fatal("corrupt file blocked Take")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

// A config saved before Ollama web search was removed loads as auto, chains tavily then searxng, and Save drops the stale key.
func TestLegacyOllamaConfig(t *testing.T) {
	useConfig(t, `{"search":{"provider":"ollama","tavily_key":"T","ollama_key":"O"}}`)
	c := LoadConfig()
	var names []string
	for _, p := range chain(c) {
		names = append(names, p.Name())
	}
	if c.Provider != "" || strings.Join(names, ",") != "tavily,searxng" {
		t.Fatalf("provider %q chain %v, want auto and tavily,searxng", c.Provider, names)
	}
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(lang.Path()); strings.Contains(string(b), "ollama") {
		t.Fatalf("saved config still names ollama: %s", b)
	}
}
