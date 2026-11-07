package listen

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

// RestartWait spaces listen.sh restarts (C3); one more failure turns listening off.
var RestartWait = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute}

// ScriptPath is MYVECTOR_LISTEN_SCRIPT, else robot/openmic/listen.sh above the working directory.
func ScriptPath() (string, error) {
	if p := os.Getenv("MYVECTOR_LISTEN_SCRIPT"); p != "" {
		return p, nil
	}
	return conf.FindUp(filepath.Join("robot", "openmic", "listen.sh"))
}

// Runner keeps one listen.sh per listening robot (C2, C3).
type Runner struct {
	Script func() (string, error)
	Off    func(esn, why string) // listening off after the capture gave up
	Log    func(string)
	Wait   []time.Duration // nil = RestartWait

	mu    sync.Mutex
	procs map[string]*proc
}

type proc struct {
	stop    chan struct{}
	cmd     *exec.Cmd
	clipped bool
}

// Start runs listen.sh <esn> unless it already runs.
func (r *Runner) Start(esn string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.procs == nil {
		r.procs = map[string]*proc{}
	}
	if _, ok := r.procs[esn]; ok {
		return
	}
	p := &proc{stop: make(chan struct{})}
	r.procs[esn] = p
	go r.loop(esn, p)
}

// Stop kills esn's listen.sh process group; no restart follows.
func (r *Runner) Stop(esn string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.procs[esn]
	if !ok {
		return
	}
	delete(r.procs, esn)
	close(p.stop)
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	}
}

// Clipped marks a clip that reached the server: the restart count starts over.
func (r *Runner) Clipped(esn string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.procs[esn]; ok {
		p.clipped = true
	}
}

func (r *Runner) loop(esn string, p *proc) {
	wait := r.Wait
	if wait == nil {
		wait = RestartWait
	}
	for fails := 0; ; {
		code, last := r.run(esn, p)
		r.mu.Lock()
		stopped := r.procs[esn] != p
		if p.clipped {
			fails, p.clipped = 0, false
		}
		r.mu.Unlock()
		switch {
		case stopped:
			return
		case code == 0: // 410: listening already off
			r.forget(esn, p)
			return
		case code == 2 || fails == len(wait):
			r.forget(esn, p)
			r.Log("myvector listen: listen.sh gave up: " + last)
			r.Off(esn, "capture")
			return
		}
		r.Log("myvector listen: listen.sh " + last + "; restart in " + wait[fails].String())
		select {
		case <-p.stop:
			return
		case <-time.After(wait[fails]):
		}
		fails++
	}
}

func (r *Runner) forget(esn string, p *proc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.procs[esn] == p {
		delete(r.procs, esn)
	}
}

// run starts one listen.sh unless p was stopped; returns its exit code (-1 = no start) and a log tail.
func (r *Runner) run(esn string, p *proc) (int, string) {
	script, err := r.Script()
	if err != nil {
		return 2, err.Error()
	}
	var out lastLine
	cmd := exec.Command(script, esn)
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	r.mu.Lock()
	if r.procs[esn] != p {
		r.mu.Unlock()
		return -1, ""
	}
	err = cmd.Start()
	p.cmd = cmd
	r.mu.Unlock()
	if err != nil {
		return 2, err.Error()
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, ""
	case errors.As(err, &exit):
		return exit.ExitCode(), exit.String() + ": " + out.String()
	}
	return 1, err.Error()
}

// lastLine keeps the last non-empty output line.
type lastLine struct {
	mu   sync.Mutex
	line string
}

func (l *lastLine) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range strings.Split(string(b), "\n") {
		if s = strings.TrimSpace(s); s != "" {
			l.line = s
		}
	}
	return len(b), nil
}

func (l *lastLine) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.line
}
