package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateKeepsOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	if err := Update(path, map[string]any{"t1": "cori", "slow": 1.5}); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, map[string]any{"lang": "et-EE"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if want := "{\n  \"lang\": \"et-EE\",\n  \"slow\": 1.5,\n  \"t1\": \"cori\"\n}\n"; string(b) != want {
		t.Fatalf("file = %q, want %q", b, want)
	}
}

func TestUpdateReplacesUnparsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	for _, bad := range []string{"{", "null", "[1]"} {
		os.WriteFile(path, []byte(bad), 0644)
		if err := Update(path, map[string]any{"lang": "en-US"}); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(path); string(b) != "{\n  \"lang\": \"en-US\"\n}\n" {
			t.Errorf("after %s: %q", bad, b)
		}
	}
}

func TestUpdateFailLeavesNoTemp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	os.Mkdir(path, 0755) // rename onto a directory fails
	if err := Update(path, map[string]any{"lang": "en-US"}); err == nil {
		t.Fatal("Update over a directory succeeded")
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".myvectorConfig-*")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}
