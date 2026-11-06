package botinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "botSdkInfo.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFirstRobot(t *testing.T) {
	p := write(t, `{"robots":[{"esn":"00e1","ip_address":"10.0.0.5","guid":"g1"},{"esn":"00e2"}]}`)
	r, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Robot{Esn: "00e1", IPAddress: "10.0.0.5", GUID: "g1"}); r != want {
		t.Fatalf("Load = %+v, want %+v", r, want)
	}
}

func TestLoadNoRobots(t *testing.T) {
	if _, err := Load(write(t, `{"robots":[]}`)); err == nil {
		t.Fatal("zero robots: want error")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("missing file: want error")
	}
}

func TestPathHonoursEnv(t *testing.T) {
	t.Setenv("BOTINFO", "/x/y.json")
	if got := Path(); got != "/x/y.json" {
		t.Fatalf("Path = %q", got)
	}
	t.Setenv("BOTINFO", "")
	if got := Path(); filepath.Base(got) != "botSdkInfo.json" {
		t.Fatalf("default Path = %q", got)
	}
}
