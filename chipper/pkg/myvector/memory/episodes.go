package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Episode kinds.
const (
	Chat   = "chat"
	Intent = "intent"
)

// Episode is one captured turn, text verbatim (M6).
type Episode struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Intent string    `json:"intent"`
	User   string    `json:"user"`
	Reply  string    `json:"reply"`
	Lang   string    `json:"lang"`
}

// Now is the memory clock; a var so tests pin it.
var Now = time.Now

var epMu sync.Mutex

// EpisodePath is esn's JSONL for day (YYYY-MM-DD in the robot's zone), under myvectorMemory/ beside Path().
func EpisodePath(esn, day string) string {
	return filepath.Join(filepath.Dir(Path()), "myvectorMemory", esn, day+".jsonl")
}

// Capture appends e to esn's file for e.At's day in zone; a no-op when Off().
func Capture(esn string, e Episode, zone *time.Location) error {
	if Off() {
		return nil
	}
	if esn == "" || esn == "." || esn == ".." || strings.ContainsAny(esn, `/\`) {
		return fmt.Errorf("memory capture: bad esn %q", esn)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := EpisodePath(esn, e.At.In(zone).Format("2006-01-02"))
	epMu.Lock()
	defer epMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

var (
	turnMu   sync.Mutex
	lastTurn = time.Now()
)

// Touch stamps LastTurn with Now() (M3, M4, M19).
func Touch() {
	turnMu.Lock()
	lastTurn = Now()
	turnMu.Unlock()
}

// LastTurn is the latest Touch, else process start.
func LastTurn() time.Time {
	turnMu.Lock()
	defer turnMu.Unlock()
	return lastTurn
}

// pendingTTL drops a MarkLLM entry no intent capture consumed.
const pendingTTL = time.Minute

type llmKey struct{ esn, text string }

var (
	pendMu  sync.Mutex
	pending = map[llmKey]time.Time{}
)

// MarkLLM records that (esn, text) went to the LLM, whose chat capture holds the turn (M4).
func MarkLLM(esn, text string) {
	pendMu.Lock()
	defer pendMu.Unlock()
	now := Now()
	prunePending(now)
	pending[llmKey{esn, text}] = now
}

// TakeLLM reports and consumes a MarkLLM of (esn, text) younger than pendingTTL.
func TakeLLM(esn, text string) bool {
	pendMu.Lock()
	defer pendMu.Unlock()
	prunePending(Now())
	k := llmKey{esn, text}
	_, ok := pending[k]
	delete(pending, k)
	return ok
}

func prunePending(now time.Time) {
	for k, at := range pending {
		if now.Sub(at) >= pendingTTL {
			delete(pending, k)
		}
	}
}
