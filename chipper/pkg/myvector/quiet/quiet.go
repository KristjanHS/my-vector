// Package quiet is the "shut up" sound lock: the whole-phrase matcher and the lock file every robot sound path checks.
package quiet

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Phrases stop the robot when a request is exactly one of them after filler removal (every saved language).
var Phrases = []string{"shut up", "shutup", "turn off", "ole vait", "jää vait", "vait", "lülita välja", "lülitu välja"}

// Fillers are dropped before matching.
var Fillers = []string{"hey", "hei", "vector", "victor", "vektor", "please", "palun", "now", "nüüd"}

const (
	LockFor = 2 * time.Hour
	NapFor  = 2 * time.Hour
)

func tokens(text string) []string {
	fillers := map[string]bool{}
	for _, f := range Fillers {
		fillers[f] = true
	}
	var out []string
	for _, t := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	}) {
		if t = strings.Trim(t, "'"); t != "" && !fillers[t] {
			out = append(out, t)
		}
	}
	return out
}

// Match reports the phrase text equals once lowercased, de-punctuated and stripped of fillers.
func Match(text string) (string, bool) {
	got := strings.Join(tokens(text), " ")
	for _, p := range Phrases {
		if got == p {
			return p, true
		}
	}
	return "", false
}

// MatchAny reports a phrase found as a whole-word run anywhere in text (open-mic clips carry the robot's own speech).
func MatchAny(text string) (string, bool) {
	got := " " + strings.Join(tokens(text), " ") + " "
	for _, p := range Phrases {
		if strings.Contains(got, " "+p+" ") {
			return p, true
		}
	}
	return "", false
}

// Path is the lock file: $MYVECTOR_QUIET_FILE, else quiet-until in robot/work (repo root or chipper/ cwd); "" when neither exists.
func Path() string {
	if p := os.Getenv("MYVECTOR_QUIET_FILE"); p != "" {
		return p
	}
	for _, d := range []string{"robot/work", "../robot/work"} {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return filepath.Join(d, "quiet-until")
		}
	}
	return ""
}

// Until reads the lock; a missing file is the zero time.
func Until() (time.Time, string, error) {
	p := Path()
	if p == "" {
		return time.Time{}, "", nil
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return time.Time{}, "", nil
	} else if err != nil {
		return time.Time{}, "", err
	}
	f := strings.SplitN(strings.TrimSpace(string(b)), " ", 2)
	sec, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("quiet: bad lock file %s: %q", p, b)
	}
	src := ""
	if len(f) == 2 {
		src = f[1]
	}
	return time.Unix(sec, 0), src, nil
}

// Set locks until max(current, until), written tmp+rename so the root server and user scripts never see half a file.
func Set(until time.Time, source string) error {
	p := Path()
	if p == "" {
		return fmt.Errorf("quiet: no robot/work dir and no $MYVECTOR_QUIET_FILE")
	}
	if cur, _, err := Until(); err == nil && !cur.Before(until) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".quiet-until-*")
	if err != nil {
		return err
	}
	source = strings.Join(strings.Fields(source), " ")
	if _, err = fmt.Fprintf(tmp, "%d %s\n", until.Unix(), source); err == nil {
		err = tmp.Chmod(0o666)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}
