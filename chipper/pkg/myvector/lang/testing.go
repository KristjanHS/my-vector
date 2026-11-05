package lang

import (
	"path/filepath"
	"testing"
)

// UseForTest saves l in a temp settings file for t's duration; cleanup restores Path and clears the cache.
func UseForTest(t testing.TB, l string) {
	t.Helper()
	old := Path
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	Path = func() string { return path }
	t.Cleanup(func() { Path = old; cur.Store(nil) })
	if err := Save(l); err != nil {
		t.Fatal(err)
	}
}
