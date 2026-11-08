package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScore(t *testing.T) {
	for _, c := range []struct {
		answer string
		words  []string
		want   float64
	}{
		{"A cup on a table.", []string{"cup", "table"}, 1},
		{"Nothing here.", []string{"cup", "table"}, 0},
		{"Laual on kaks Tassid.", []string{"tass"}, 1},
		{"A CUP.", []string{"Cup", "table"}, 0.5},
		{"teacup", []string{"cup"}, 0},
	} {
		if got := score(c.answer, c.words); got != c.want {
			t.Errorf("score(%q, %v) = %v, want %v", c.answer, c.words, got, c.want)
		}
	}
}

func TestLoadChecklist(t *testing.T) {
	write := func(t *testing.T, list string, photos ...string) string {
		d := t.TempDir()
		for _, p := range photos {
			os.WriteFile(filepath.Join(d, p), []byte{0xff, 0xd8}, 0o644)
		}
		os.WriteFile(filepath.Join(d, "checklist.json"), []byte(list), 0o644)
		return d
	}
	ok := `[{"photo":"01.jpg","et":["tass"],"en":["cup"]}]`
	if items, err := loadChecklist(write(t, ok, "01.jpg")); err != nil || len(items) != 1 || items[0].ET[0] != "tass" {
		t.Fatalf("good checklist: %v %v", items, err)
	}
	for name, c := range map[string]struct{ list, want string }{
		"missing photo": {ok, "01.jpg"},
		"empty et":      {`[{"photo":"01.jpg","et":[],"en":["cup"]}]`, "non-empty"},
		"no en":         {`[{"photo":"01.jpg","et":["tass"]}]`, "non-empty"},
	} {
		photos := []string{"01.jpg"}
		if name == "missing photo" {
			photos = nil
		}
		if _, err := loadChecklist(write(t, c.list, photos...)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want it to name %q", name, err, c.want)
		}
	}
}
