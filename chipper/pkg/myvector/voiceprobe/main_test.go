package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

// TestRunQuiet: both sound commands refuse with the QUIET line before connect (BOTINFO points nowhere, so a missed gate fails on the read, never on the robot).
func TestRunQuiet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOTINFO", filepath.Join(dir, "none.json"))
	t.Setenv("MYVECTOR_QUIET_FILE", filepath.Join(dir, "quiet-until"))
	if err := quiet.Set(time.Now().Add(time.Hour), "test"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"say", "play"} {
		if err := run([]string{"voiceprobe", c, "x"}); err == nil || !strings.HasPrefix(err.Error(), "QUIET: robot sound locked until ") {
			t.Errorf("%s locked: %v, want the QUIET line", c, err)
		}
	}
}
