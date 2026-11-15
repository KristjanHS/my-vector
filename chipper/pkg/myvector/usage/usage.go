// Package usage is the prod-evidence usage ledger (claudeconf plan 2026-11-15-my-vector-prod-evidence.md, Stage 2):
// one TSV row per local day — `day real test unplanned_restarts tailnet` — counts only, never an IP, ESN, hostname or
// transcript. The speech path counts real vs test requests (Count); the robot poll (Stage 2.2) sets the restart and
// tailnet columns of a day (Update) without touching its counters.
package usage

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Header is the ledger's first line.
const Header = "day\treal\ttest\tunplanned_restarts\ttailnet"

// Tailnet column values: TailnetUnknown until the Stage 2.2 poll writes TailnetUp or TailnetDown.
const (
	TailnetUnknown = "-"
	TailnetUp      = "up"
	TailnetDown    = "down"
)

// Kind is where one speech request counts.
type Kind int

const (
	None Kind = iota // counted nowhere: a false wake (non-loopback peer, empty transcript)
	Real             // a non-loopback peer with a non-empty transcript: the robot, used for real
	Test             // a loopback or unknown peer: fakebot, scripts/live-check.sh
)

// Classify sorts a speech request by its gRPC peer IP and its STT transcript. The robot reaches wire-pod over LAN or
// tailnet, never over lo (WSL mirrored networking keeps the robot's source IP); fakebot and live-check dial 127.0.0.1
// with the robot's real ESN, so the peer, not the ESN, is the discriminator. A nil peer (no peer in the stream context,
// a non-IP address) cannot be shown to be the robot, so it counts Test — never Real — whatever its transcript.
func Classify(peer net.IP, transcript string) Kind {
	if peer == nil || peer.IsLoopback() {
		return Test
	}
	if strings.TrimSpace(transcript) == "" {
		return None
	}
	return Real
}

// AddrIP is a net.Addr's IP (a gRPC peer.Peer.Addr), nil when it has none (a unix socket, an unparsable address).
func AddrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case nil:
		return nil
	case *net.TCPAddr:
		return v.IP
	case *net.UDPAddr:
		return v.IP
	}
	s := a.String()
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	return net.ParseIP(s)
}

// Row is one ledger day.
type Row struct {
	Day               string // local date, YYYY-MM-DD
	Real              int
	Test              int
	UnplannedRestarts int
	Tailnet           string // TailnetUnknown, TailnetUp or TailnetDown
}

// Day is t's date in t's own location; callers pass time.Now() (local), tests a pinned time.
func Day(t time.Time) string { return t.Format("2006-01-02") }

var dayRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// check keeps every written field a date, a count or a tailnet word, so nothing else (an IP, a name) can reach the file.
func (r Row) check() error {
	if !dayRe.MatchString(r.Day) {
		return fmt.Errorf("usage: bad day %q", r.Day)
	}
	if r.Real < 0 || r.Test < 0 || r.UnplannedRestarts < 0 {
		return fmt.Errorf("usage: negative count on %s", r.Day)
	}
	switch r.Tailnet {
	case TailnetUnknown, TailnetUp, TailnetDown:
		return nil
	}
	return fmt.Errorf("usage: bad tailnet %q on %s", r.Tailnet, r.Day)
}

// fileMu serializes every read-modify-write in the process, whichever Ledger value (counter or poll) does it.
var fileMu sync.Mutex

// Ledger is the TSV file at Path. Each Count/Update reads the file, changes one row and replaces the file (temp +
// rename) before returning, so a wire-pod restart loses nothing.
type Ledger struct{ Path string }

// Open returns the ledger at path; it does no I/O (a missing file reads as no rows).
func Open(path string) *Ledger { return &Ledger{Path: path} }

// Count adds one request of kind k to now's day; None is a no-op that touches no file.
func (l *Ledger) Count(k Kind, now time.Time) error {
	switch k {
	case Real:
		return l.Update(Day(now), func(r *Row) { r.Real++ })
	case Test:
		return l.Update(Day(now), func(r *Row) { r.Test++ })
	}
	return nil
}

// Update applies fn to day's row (a new row has zero counts and tailnet "-") and writes the ledger back, rows in day
// order. A row fn leaves invalid (bad day, negative count, tailnet not -/up/down) is an error and nothing is written.
func (l *Ledger) Update(day string, fn func(*Row)) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	rows, err := l.read()
	if err != nil {
		return err
	}
	i := sort.Search(len(rows), func(i int) bool { return rows[i].Day >= day })
	if i == len(rows) || rows[i].Day != day {
		rows = append(rows, Row{})
		copy(rows[i+1:], rows[i:])
		rows[i] = Row{Day: day, Tailnet: TailnetUnknown}
	}
	fn(&rows[i])
	if rows[i].Day != day {
		return fmt.Errorf("usage: Update of %s changed its day to %q", day, rows[i].Day)
	}
	if err := rows[i].check(); err != nil {
		return err
	}
	return l.write(rows)
}

// Rows is the ledger's rows in day order (none when the file does not exist yet).
func (l *Ledger) Rows() ([]Row, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return l.read()
}

// read parses the file; a malformed line is an error, so a ledger it cannot read is never overwritten.
func (l *Ledger) read() ([]Row, error) {
	b, err := os.ReadFile(l.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []Row
	sc := bufio.NewScanner(bytes.NewReader(b))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || line == Header {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("usage: %s:%d: want 5 fields, got %d", l.Path, n, len(f))
		}
		r := Row{Day: f[0], Tailnet: f[4]}
		for j, p := range []*int{&r.Real, &r.Test, &r.UnplannedRestarts} {
			if *p, err = strconv.Atoi(f[1+j]); err != nil {
				return nil, fmt.Errorf("usage: %s:%d: %w", l.Path, n, err)
			}
		}
		if err := r.check(); err != nil {
			return nil, fmt.Errorf("%w (%s:%d)", err, l.Path, n)
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Day < rows[j].Day })
	return rows, sc.Err()
}

func (l *Ledger) write(rows []Row) error {
	var b strings.Builder
	b.WriteString(Header + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%d\t%d\t%d\t%s\n", r.Day, r.Real, r.Test, r.UnplannedRestarts, r.Tailnet)
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.Path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op after the rename
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), l.Path)
}
