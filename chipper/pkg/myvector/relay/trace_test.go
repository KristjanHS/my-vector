package relay

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// NewTrace keeps the newest TraceKeep turn folders and never touches other entries.
func TestNewTracePrunes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", root)
	defer func(k int) { TraceKeep = k }(TraceKeep)
	TraceKeep = 2
	os.Mkdir(filepath.Join(root, "keep-me"), 0o755)
	t0 := time.Date(2026, 11, 10, 17, 0, 0, 0, time.Local)
	for i := 0; i < 4; i++ {
		if NewTrace(t0.Add(time.Duration(i)*time.Second)) == nil {
			t.Fatal("trace off")
		}
	}
	es, _ := os.ReadDir(root)
	var got []string
	for _, e := range es {
		got = append(got, e.Name())
	}
	want := []string{"20261110-170002.000", "20261110-170003.000", "keep-me"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("dirs %v, want %v", got, want)
	}
}

// A nil trace (tracing off) takes every call.
func TestNilTrace(t *testing.T) {
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", filepath.Join(t.TempDir(), "absent"))
	tr := NewTrace(time.Now())
	if tr != nil {
		t.Fatal("trace on without its dir")
	}
	tr.Note("h", "x")
	tr.Request("s", map[string]any{"messages": []any{}})
	tr.Reply("s", "m", 0, "x", false, nil)
}
