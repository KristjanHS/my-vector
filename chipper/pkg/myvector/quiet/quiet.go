// Package quiet is the "shut up" sound lock: the whole-phrase matcher and the lock file every robot sound path checks.
package quiet

import (
	"errors"
	"fmt"
	"net/http"
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

// Tokens lowercases text, splits on non-letters/digits and drops Fillers (listen matches on it too).
func Tokens(text string) []string {
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
	got := strings.Join(Tokens(text), " ")
	for _, p := range Phrases {
		if got == p {
			return p, true
		}
	}
	return "", false
}

// MatchAny reports a phrase found as a whole-word run anywhere in text (open-mic clips carry the robot's own speech).
func MatchAny(text string) (string, bool) {
	got := " " + strings.Join(Tokens(text), " ") + " "
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

// Line is the QUIET refusal when the lock holds at now; an unreadable lock file reads as unlocked (fail-open, as Set treats it).
func Line(now time.Time) (string, bool) {
	u, src, err := Until()
	if err != nil || !now.Before(u) {
		return "", false
	}
	return fmt.Sprintf("QUIET: robot sound locked until %s (%s); make unquiet lifts it", u.Local().Format("15:04"), src), true
}

// Refuse writes 423 + Line and reports true while locked; every server robot-sound handler calls it first.
func Refuse(w http.ResponseWriter) bool {
	line, locked := Line(time.Now())
	if locked {
		http.Error(w, line, http.StatusLocked)
	}
	return locked
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

// NapURL is vic-engine's console on the robot; tests point it at httptest.
var NapURL = func(ip string) string { return "http://" + ip + ":8888" }

// NapTimeout bounds the nap request; the voice reply never waits on it.
var NapTimeout = 5 * time.Second

// ErrNapOff: $MYVECTOR_NAP=off (routeprobe, tests) — nothing is sent.
var ErrNapOff = errors.New("quiet: nap off ($MYVECTOR_NAP=off)")

// Nap sets the firmware's SleepCycle_NapUntil_s to until (0 ends the nap); patch 0011 holds the night rules until then.
func Nap(ip string, until time.Time) error {
	if os.Getenv("MYVECTOR_NAP") == "off" {
		return ErrNapOff
	}
	sec := int64(0)
	if !until.IsZero() {
		sec = until.Unix()
	}
	c := http.Client{Timeout: NapTimeout}
	r, err := c.Get(NapURL(ip) + "/consolevarset?key=SleepCycle_NapUntil_s&value=" + strconv.FormatInt(sec, 10))
	if err != nil {
		return err
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("quiet: nap: vic-engine %s", r.Status)
	}
	return nil
}
