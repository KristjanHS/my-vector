package listen

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type runRig struct {
	r   *Runner
	dir string
	mu  sync.Mutex
	off []string
}

// newRunRig: a Runner on a stub script with body; "$D" is the rig dir, each run appends a line to $D/runs.
func newRunRig(t *testing.T, body string) *runRig {
	t.Helper()
	g := &runRig{dir: t.TempDir()}
	script := filepath.Join(g.dir, "listen.sh")
	src := "#!/bin/sh\nD=" + g.dir + "\necho \"$1\" >>$D/runs\n" + body + "\n"
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	g.r = &Runner{
		Script: func() (string, error) { return script, nil },
		Off: func(esn, why string) {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.off = append(g.off, esn+" "+why)
		},
		Log:  func(string) {},
		Wait: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond},
	}
	return g
}

func (g *runRig) runs() int {
	b, _ := os.ReadFile(filepath.Join(g.dir, "runs"))
	return strings.Count(string(b), "\n")
}

func (g *runRig) offs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.off...)
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal(what)
}

func alive(pidFile string) bool {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func TestRunnerStopsOnOff(t *testing.T) {
	g := newRunRig(t, "sleep 60 & echo $! >$D/child\nwait")
	s := NewState()
	s.OnOff = func(esn string) { g.r.Stop(esn) }
	s.Activate(esn, time.Now())
	g.r.Start(esn)
	g.r.Start(esn)
	child := filepath.Join(g.dir, "child")
	eventually(t, "stub never started", func() bool { return alive(child) })
	s.Deactivate(esn, "phrase")
	eventually(t, "child of listen.sh outlived off", func() bool { return !alive(child) })
	time.Sleep(50 * time.Millisecond)
	if n := g.runs(); n != 1 {
		t.Fatalf("runs = %d, want 1 (no restart after off, no second Start)", n)
	}
	if o := g.offs(); len(o) != 0 {
		t.Fatalf("Off called: %v", o)
	}
}

func TestRunnerRestarts(t *testing.T) {
	g := newRunRig(t, "exit 1")
	g.r.Start(esn)
	eventually(t, "never gave up", func() bool { return len(g.offs()) == 1 })
	if n, o := g.runs(), g.offs(); n != 1+len(g.r.Wait) || o[0] != esn+" capture" {
		t.Fatalf("runs = %d, off = %v; want %d runs then %q", n, o, 1+len(g.r.Wait), esn+" capture")
	}

	g = newRunRig(t, "sleep 0.3; exit 1")
	g.r.Start(esn)
	eventually(t, "second run never started", func() bool { return g.runs() == 2 })
	g.r.Clipped(esn)
	eventually(t, "never gave up after the reset", func() bool { return len(g.offs()) == 1 })
	if n := g.runs(); n != 2+len(g.r.Wait) {
		t.Fatalf("runs = %d, want %d (a clip resets the count)", n, 2+len(g.r.Wait))
	}

	g = newRunRig(t, "exit 2")
	g.r.Start(esn)
	eventually(t, "SETUP exit never turned off", func() bool { return len(g.offs()) == 1 })
	if n := g.runs(); n != 1 {
		t.Fatalf("runs = %d, want 1 (exit 2 is not retried)", n)
	}

	g = newRunRig(t, "exit 0")
	g.r.Start(esn)
	eventually(t, "410 exit never ended the run", func() bool {
		g.r.mu.Lock()
		defer g.r.mu.Unlock()
		return len(g.r.procs) == 0
	})
	if n, o := g.runs(), g.offs(); n != 1 || len(o) != 0 {
		t.Fatalf("410 exit: runs = %d, off = %v; want 1 run, no Off", n, o)
	}
}
