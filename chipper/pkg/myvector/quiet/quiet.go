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

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
)

// Phrases stop the robot when a request is exactly one of them after filler removal (every saved language).
var Phrases = []string{"shut up", "shutup", "turn off", "ole vait", "jää vait", "vait", "lülita välja", "lülitu välja"}

// Fillers are dropped before matching.
var Fillers = []string{"hey", "hei", "vector", "victor", "vektor", "please", "palun", "now", "nüüd"}

const (
	// LockFor is a voice "shut up"'s lock; the nap runs to the lock's end (an existing longer lock wins).
	LockFor = 2 * time.Hour
	// LockVolume is master_volume while locked (vectorpb Volume_LOW; ruled V1: low, not mute).
	LockVolume = 1
	// MaxVolume is vectorpb Volume_HIGH.
	MaxVolume = 5
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

// NoVolume is a lock file's "-" volume field: nothing saved to restore.
const NoVolume = -1

// Lock is the lock file `<until-epoch> <vol|-> <source>`; Vol is the master_volume to restore, or NoVolume.
type Lock struct {
	Until  time.Time
	Vol    int
	Source string
}

// Read reads the lock; a missing file is the zero Lock. The 2-field `<until> <source>` form reads as Vol NoVolume.
func Read() (Lock, error) {
	none := Lock{Vol: NoVolume}
	p := Path()
	if p == "" {
		return none, nil
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return none, nil
	} else if err != nil {
		return none, err
	}
	head, rest, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	sec, err := strconv.ParseInt(head, 10, 64)
	if err != nil {
		return none, fmt.Errorf("quiet: bad lock file %s: %q", p, b)
	}
	l := Lock{Until: time.Unix(sec, 0), Vol: NoVolume, Source: rest}
	// Same split as guard.sh quiet_locked: `-` or an integer, then a space, is the volume field.
	if tok, src, ok := strings.Cut(rest, " "); ok && (tok == "-" || isUint(tok)) {
		l.Source = src
		if v, _ := strconv.Atoi(tok); tok != "-" && v >= 0 && v <= MaxVolume {
			l.Vol = v
		}
	}
	return l, nil
}

func isUint(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Until reads the lock's end and source; a missing file is the zero time.
func Until() (time.Time, string, error) {
	l, err := Read()
	return l.Until, l.Source, err
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

// Set locks until max(current, until) with no volume to save (SetVol NoVolume).
func Set(until time.Time, source string) error { return SetVol(until, NoVolume, source) }

// SetVol locks until max(current, until), written tmp+rename so the root server and user scripts never see half a file.
// A volume already saved in the file wins over vol: an extend keeps the first.
func SetVol(until time.Time, vol int, source string) error {
	p := Path()
	if p == "" {
		return fmt.Errorf("quiet: no robot/work dir and no $MYVECTOR_QUIET_FILE")
	}
	if vol < NoVolume || vol > MaxVolume {
		return fmt.Errorf("quiet: volume %d not in 0..%d", vol, MaxVolume)
	}
	if cur, err := Read(); err == nil && !cur.Until.IsZero() {
		if cur.Vol != NoVolume {
			vol = cur.Vol
		}
		if !cur.Until.Before(until) {
			if cur.Vol == vol {
				return nil
			}
			until, source = cur.Until, cur.Source
		}
	}
	return write(p, Lock{Until: until, Vol: vol, Source: source})
}

// Hold writes l as is (no max): a failed volume restore leaves an expired file keeping l.Vol, which reads as unlocked.
func Hold(l Lock) error {
	p := Path()
	if p == "" {
		return fmt.Errorf("quiet: no robot/work dir and no $MYVECTOR_QUIET_FILE")
	}
	if l.Vol < NoVolume || l.Vol > MaxVolume {
		return fmt.Errorf("quiet: volume %d not in 0..%d", l.Vol, MaxVolume)
	}
	return write(p, l)
}

func write(p string, l Lock) error {
	source := strings.Join(strings.Fields(l.Source), " ")
	if source == "" {
		source = "unknown"
	}
	v := "-"
	if l.Vol != NoVolume {
		v = strconv.Itoa(l.Vol)
	}
	return conf.WriteFile(p, ".quiet-until-*", []byte(fmt.Sprintf("%d %s %s\n", l.Until.Unix(), v, source)), 0o666)
}

// Clear removes the lock file; a missing one is no error.
func Clear() error {
	p := Path()
	if p == "" {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// NapURL is vic-engine's console on the robot; tests point it at httptest.
var NapURL = func(ip string) string { return "http://" + ip + ":8888" }

// NapTimeout bounds the nap request; the voice reply never waits on it.
var NapTimeout = 5 * time.Second

// ErrNapOff: $MYVECTOR_NAP=off (routeprobe, tests) — no nap and no volume change is sent.
var ErrNapOff = errors.New("quiet: nap off ($MYVECTOR_NAP=off)")

// RobotOff is $MYVECTOR_NAP=off: callers return ErrNapOff instead of touching the robot.
func RobotOff() bool { return os.Getenv("MYVECTOR_NAP") == "off" }

// Nap sets the firmware's SleepCycle_NapUntil_s to until (0 ends the nap); patch 0011 holds the night rules until then.
func Nap(ip string, until time.Time) error {
	if RobotOff() {
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
