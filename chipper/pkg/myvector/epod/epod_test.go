package epod

import (
	"os"
	"path/filepath"
	"testing"
)

func dirWith(t *testing.T, files map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for n, s := range files {
		if err := os.WriteFile(filepath.Join(d, n), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func catchLog(t *testing.T) *[]string {
	t.Helper()
	var got []string
	old := logln
	logln = func(a ...any) {
		for _, x := range a {
			got = append(got, x.(string))
		}
	}
	t.Cleanup(func() { logln = old })
	return &got
}

func TestPairOwn(t *testing.T) {
	logs := catchLog(t)
	own := dirWith(t, map[string]string{"ep.crt": "OWN-C", "ep.key": "OWN-K"})
	up := dirWith(t, map[string]string{"ep.crt": "UP-C", "ep.key": "UP-K"})
	c, k, isOwn, err := PairFrom(own, up)
	if err != nil || !isOwn || string(c) != "OWN-C" || string(k) != "OWN-K" || len(*logs) != 0 {
		t.Fatalf("got %q %q own=%v err=%v logs=%q, want own pair, no log", c, k, isOwn, err, *logs)
	}
}

func TestPairFallback(t *testing.T) {
	logs := catchLog(t)
	up := dirWith(t, map[string]string{"ep.crt": "UP-C", "ep.key": "UP-K"})
	c, k, isOwn, err := PairFrom(filepath.Join(t.TempDir(), "absent"), up)
	if err != nil || isOwn || string(c) != "UP-C" || string(k) != "UP-K" {
		t.Fatalf("got %q %q own=%v err=%v, want upstream pair", c, k, isOwn, err)
	}
	if len(*logs) != 1 || (*logs)[0] != PublicLog {
		t.Fatalf("logs = %q, want exactly PublicLog", *logs)
	}
}

func TestPairHalf(t *testing.T) {
	up := dirWith(t, map[string]string{"ep.crt": "UP-C", "ep.key": "UP-K"})
	for _, half := range []string{"ep.crt", "ep.key"} {
		c, k, _, err := PairFrom(dirWith(t, map[string]string{half: "X"}), up)
		if err == nil || c != nil || k != nil {
			t.Errorf("%s only: got %q %q err=%v, want an error and no pair", half, c, k, err)
		}
	}
}

func TestDirEnvOverride(t *testing.T) {
	t.Setenv("MYVECTOR_EPOD_DIR", "/x/y")
	if Dir() != "/x/y" {
		t.Fatalf("Dir() = %q with MYVECTOR_EPOD_DIR set", Dir())
	}
	t.Setenv("MYVECTOR_EPOD_DIR", "")
	if Dir() != DefaultDir {
		t.Fatalf("Dir() = %q, want DefaultDir", Dir())
	}
}
