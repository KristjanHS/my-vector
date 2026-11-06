package lang

import (
	"os"
	"path/filepath"
	"strings"
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

// Falsifier for the init-order bug: Current() first runs at package init, before vars.Init moves ApiConfigPath.
func TestLangCurrentFollowsPath(t *testing.T) {
	first := tempPath(t)
	os.WriteFile(first, []byte(`{"lang":"en-US"}`), 0644)
	if got := Current(); got != English {
		t.Fatalf("first file: %q", got)
	}
	second := filepath.Join(t.TempDir(), "myvectorConfig.json")
	os.WriteFile(second, []byte(`{"lang":"et-EE"}`), 0644)
	Path = func() string { return second }
	if got := Current(); got != Estonian {
		t.Fatalf("after Path change: %q, want the new file's %q", got, Estonian)
	}
}

func TestLangETModel(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{``, DefaultETModel}, // no file
		{`{"lang":"et-EE"}`, DefaultETModel},
		{`{"et_model":""}`, DefaultETModel},
		{`{"et_model":7}`, DefaultETModel},
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
	if err := Save(Estonian); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != "estllm:kept" {
		t.Fatalf("ETModel() after Save = %q", got)
	}
}

func TestLangSaveETModel(t *testing.T) {
	path := tempPath(t)
	os.WriteFile(path, []byte(`{"lang":"et-EE","t1":"cori"}`), 0644)
	Current() // warm the cache: SaveETModel must replace it, not leave the old model
	if err := SaveETModel("  estllm:new  "); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != "estllm:new" {
		t.Fatalf("ETModel() after SaveETModel = %q, want the trimmed value", got)
	}
	b, _ := os.ReadFile(path)
	if want := "{\n  \"et_model\": \"estllm:new\",\n  \"lang\": \"et-EE\",\n  \"t1\": \"cori\"\n}\n"; string(b) != want {
		t.Fatalf("file = %q, want %q", b, want)
	}
	if Current() != Estonian {
		t.Fatalf("SaveETModel changed Current to %q", Current())
	}
	for _, bad := range []string{"estllm: new", "est\tllm", strings.Repeat("m", ETModelMax+1)} {
		if err := SaveETModel(bad); err == nil {
			t.Errorf("SaveETModel(%.20q) accepted", bad)
		}
	}
	if got := ETModel(); got != "estllm:new" {
		t.Fatalf("a rejected SaveETModel changed ETModel to %q", got)
	}
	if err := SaveETModel(strings.Repeat("é", ETModelMax)); err != nil { // the limit is characters, not bytes
		t.Fatalf("%d characters: %v", ETModelMax, err)
	}
	if err := SaveETModel(" "); err != nil {
		t.Fatal(err)
	}
	if got := ETModel(); got != DefaultETModel {
		t.Fatalf("ETModel() after an empty SaveETModel = %q, want the default", got)
	}
	cur.Store(nil) // and the file agrees after a restart
	if got := ETModel(); got != DefaultETModel {
		t.Fatalf("ETModel() reloaded after an empty SaveETModel = %q, want the default", got)
	}
}
