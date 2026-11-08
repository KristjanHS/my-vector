package memory

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SessionGap splits one ESN's turns into sessions; a session closes SessionGap after its last turn (M10).
const SessionGap = 10 * time.Minute

// ExtractRetries is how many parse failures a session is retried before it is marked done.
const ExtractRetries = 3

// Session is one ESN's run of turns with gaps < SessionGap; ID is ESN@first turn (UTC, RFC 3339).
type Session struct {
	ID, ESN string
	Turns   []Episode
}

// ReadJSONL reads one episodes file; unparsable lines are skipped with a log.
func ReadJSONL(path string) ([]Episode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Episode
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Episode
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			logf("myvector: memory episode line skipped:", path, err)
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// Episodes is every captured turn of esn, oldest first.
func Episodes(esn string) ([]Episode, error) {
	files, err := filepath.Glob(filepath.Join(filepath.Dir(EpisodePath(esn, "x")), "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Episode
	for _, p := range files {
		eps, err := ReadJSONL(p)
		if err != nil {
			return nil, err
		}
		out = append(out, eps...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// Sessions splits eps (oldest first) into esn's sessions closed at now; the open last one is left out.
func Sessions(esn string, eps []Episode, now time.Time) []Session {
	var out []Session
	for i, e := range eps {
		if i == 0 || e.At.Sub(eps[i-1].At) >= SessionGap {
			out = append(out, Session{ID: esn + "@" + e.At.UTC().Format(time.RFC3339Nano), ESN: esn})
		}
		out[len(out)-1].Turns = append(out[len(out)-1].Turns, e)
	}
	if n := len(out); n > 0 {
		if last := out[n-1].Turns; now.Sub(last[len(last)-1].At) < SessionGap {
			out = out[:n-1]
		}
	}
	return out
}

// Pending is sessions not yet in st.Extracted.
func (st *Store) Pending(sessions []Session) []Session {
	done := map[string]bool{}
	for _, id := range st.Extracted {
		done[id] = true
	}
	var out []Session
	for _, s := range sessions {
		if !done[s.ID] {
			out = append(out, s)
		}
	}
	return out
}
