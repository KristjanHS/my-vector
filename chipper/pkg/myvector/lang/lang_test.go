package lang

import (
	"os"
	"path/filepath"
	"testing"
)

// tempPath points Path at a fresh file and drops the cache.
func tempPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	old := Path
	Path = func() string { return path }
	cur.Store(nil)
	t.Cleanup(func() { Path = old; cur.Store(nil) })
	return path
}

func TestLangDefault(t *testing.T) {
	path := tempPath(t)
	if got := Current(); got != English {
		t.Fatalf("missing file: %q", got)
	}
	cur.Store(nil)
	os.WriteFile(path, []byte(`{"t1":"cori"}`), 0644) // a pre-switch file
	if got := Current(); got != English {
		t.Fatalf("no lang key: %q", got)
	}
}

func TestLangInvalidFallsBack(t *testing.T) {
	path := tempPath(t)
	for _, bad := range []string{`{`, `{"lang":"et"}`, `{"lang":""}`, `{"lang":7}`, `{"lang":"de-DE"}`} {
		cur.Store(nil)
		os.WriteFile(path, []byte(bad), 0644)
		if got := Current(); got != English {
			t.Errorf("file %s: %q, want %q", bad, got, English)
		}
	}
	cur.Store(nil)
	os.WriteFile(path, []byte(`{"lang":"et-EE"}`), 0644)
	if got := Current(); got != Estonian {
		t.Errorf("valid file: %q", got)
	}
}

func TestLangSaveKeepsVoiceKeysAndCaches(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"t1":"cori","t2":"stock","slow":1.5}`), 0644)
	if err := Save(Estonian); err != nil {
		t.Fatal(err)
	}
	if got := Current(); got != Estonian {
		t.Fatalf("Current() after Save = %q", got)
	}
	b, _ := os.ReadFile(path)
	if want := "{\n  \"lang\": \"et-EE\",\n  \"slow\": 1.5,\n  \"t1\": \"cori\",\n  \"t2\": \"stock\"\n}\n"; string(b) != want {
		t.Fatalf("file = %q", b)
	}
	if err := Save("et"); err == nil {
		t.Fatal("Save accepted an unknown language")
	}
	if got := Current(); got != Estonian {
		t.Fatalf("rejected Save changed Current: %q", got)
	}
}
