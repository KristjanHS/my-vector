package usagepoll

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/usage"
)

var loc = time.FixedZone("EET", 2*3600)

const (
	bootA = "0d2c5e1f-3a4b-4c5d-8e9f-0a1b2c3d4e5f"
	bootB = "9f8e7d6c-5b4a-4321-8fed-cba987654321"
)

// probe is Script's output for a robot with that boot_id, clock (epoch s), uptime (s) and vic-* unit lines.
func probe(boot string, now int64, uptime string, tailnet int, units ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "boot %s\nnow %d\nuptime %s\ntailnet %d\n", boot, now, uptime, tailnet)
	for _, u := range units {
		b.WriteString("unit " + u + "\n")
	}
	b.WriteString("end\n")
	return b.String()
}

// unit is a unit line whose ActiveEnter is enterSec after boot.
func unit(name string, enterSec int64, n int) string {
	return fmt.Sprintf("%s ActiveEnterTimestampMonotonic=%d NRestarts=%d", name, enterSec*1e6, n)
}

func TestParse(t *testing.T) {
	out := "settings: chatter from env.sh\n" + probe(bootA, 1763200000, "3600.25", 1,
		"vic-anim.service NRestarts=2 ActiveEnterTimestampMonotonic=12500000", "vic-engine.service ActiveEnterTimestampMonotonic=9000000")
	s, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if s.BootID != bootA || s.Now != 1763200000 || s.UptimeUS != 3600250000 || !s.Tailnet {
		t.Fatalf("got %+v", s)
	}
	if s.BootUS() != 1763200000*1e6-3600250000 {
		t.Fatalf("boot %d", s.BootUS())
	}
	if s.Units["vic-anim.service"] != (Unit{12500000, 2}) || s.Units["vic-engine.service"] != (Unit{9000000, 0}) || len(s.Units) != 2 {
		t.Fatalf("units %+v", s.Units)
	}
	bad := map[string]string{
		"no end":       strings.TrimSuffix(probe(bootA, 1, "1", 0, unit("vic-a", 1, 0)), "end\n"),
		"no unit":      probe(bootA, 1, "1", 0),
		"bad boot":     probe("my-robot", 1, "1", 0, unit("vic-a", 1, 0)),
		"bad uptime":   probe(bootA, 1, "1.2.3", 0, unit("vic-a", 1, 0)),
		"bad tailnet":  strings.Replace(probe(bootA, 1, "1", 0, unit("vic-a", 1, 0)), "tailnet 0", "tailnet yes", 1),
		"no enter":     probe(bootA, 1, "1", 0, "vic-a NRestarts=0"),
		"not vic":      probe(bootA, 1, "1", 0, unit("sshd.service", 1, 0)),
		"bad restarts": probe(bootA, 1, "1", 0, "vic-a ActiveEnterTimestampMonotonic=1 NRestarts=x"),
	}
	for name, in := range bad {
		if _, err := Parse(in); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

// snap parses a probe or fails the test.
func snap(t *testing.T, out string) Snapshot {
	t.Helper()
	s, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestDiffWindowsAndSlack(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc).Unix()
	boot := now - 36000 // uptime 10 h
	prev := snap(t, probe(bootA, now-3600, "32400", 1, unit("vic-anim", 100, 0)))
	cur := snap(t, probe(bootA, now, "36000", 1, unit("vic-anim", 30000, 0))) // restarted at boot+30000 s
	ev := boot + 30000
	cases := []struct {
		name string
		w    [2]int64
		want int
	}{
		{"no window", [2]int64{0, 0}, 1},
		{"inside", [2]int64{ev - 10, ev + 10}, 0},
		{"end+slack edge", [2]int64{ev - 500, ev - 120}, 0},
		{"past end+slack", [2]int64{ev - 500, ev - 121}, 1},
		{"start-slack edge", [2]int64{ev + 120, ev + 500}, 0},
		{"before start-slack", [2]int64{ev + 121, ev + 500}, 1},
	}
	for _, c := range cases {
		got := Diff(&prev, cur, [][2]int64{c.w}, 120*time.Second, loc)
		if got["2026-11-15"] != c.want || len(got) > c.want {
			t.Errorf("%s: got %v, want %d on 2026-11-15", c.name, got, c.want)
		}
	}
}

func TestDiffRules(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc).Unix()
	day := "2026-11-15"
	prev := snap(t, probe(bootA, now-3600, "32400", 1, unit("vic-anim", 100, 0), unit("vic-engine", 100, 1)))
	cases := []struct {
		name string
		cur  string
		w    [][2]int64
		want int
	}{
		{"unchanged", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0), unit("vic-engine", 100, 1)), nil, 0},
		{"ΔNRestarts 3", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0), unit("vic-engine", 33000, 4)), nil, 3},
		{"NRestarts reset by a manual start", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0), unit("vic-engine", 33000, 0)), nil, 1},
		{"disappeared", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0)), nil, 0},
		{"appeared, NRestarts 0", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0), unit("vic-engine", 100, 1), unit("vic-new", 33000, 0)), nil, 0},
		{"appeared, NRestarts 2", probe(bootA, now, "36000", 1, unit("vic-anim", 100, 0), unit("vic-engine", 100, 1), unit("vic-new", 33000, 2)), nil, 2},
		// rebooted 600 s ago: 1 reboot; the new boot's units starting are part of it
		{"boot_id change", probe(bootB, now, "600", 1, unit("vic-anim", 20, 0), unit("vic-engine", 25, 0)), nil, 1},
		{"boot_id change + NRestarts", probe(bootB, now, "600", 1, unit("vic-anim", 20, 0), unit("vic-engine", 300, 2)), nil, 3},
		{"boot_id change inside a window", probe(bootB, now, "600", 1, unit("vic-anim", 20, 0), unit("vic-engine", 25, 0)), [][2]int64{{now - 700, now - 500}}, 0},
	}
	for _, c := range cases {
		got := Diff(&prev, snap(t, c.cur), c.w, Slack, loc)
		total := 0
		for _, n := range got {
			total += n
		}
		if got[day] != c.want || total != c.want {
			t.Errorf("%s: got %v, want %d on %s", c.name, got, c.want, day)
		}
	}
	if got := Diff(nil, snap(t, cases[1].cur), nil, Slack, loc); len(got) != 0 {
		t.Errorf("no prev: got %v, want the baseline (none)", got)
	}
}

// fixture is a Poller on temp files with a scripted probe and a settable clock.
type fixture struct {
	*Poller
	dir string
	out string
	err error
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	f := &fixture{dir: dir}
	f.Poller = &Poller{
		Ledger:      usage.Open(filepath.Join(dir, "usage.tsv")),
		StatePath:   filepath.Join(dir, "usage-poll.state"),
		WindowsPath: filepath.Join(dir, "ship-windows.tsv"),
		ActivePath:  filepath.Join(dir, "ship-active"),
		Probe:       func(context.Context) (string, error) { return f.out, f.err },
		Now:         func() time.Time { return f.now },
	}
	return f
}

func (f *fixture) poll(t *testing.T, now time.Time, out string) {
	t.Helper()
	f.now, f.out, f.err = now, out, nil
	if err := f.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) row(t *testing.T, day string) usage.Row {
	t.Helper()
	rows, err := f.Ledger.Rows()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Day == day {
			return r
		}
	}
	return usage.Row{Day: day, Tailnet: "(no row)"}
}

func (f *fixture) write(t *testing.T, name, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPollFirstIsBaseline(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	f.poll(t, now, probe(bootA, now.Unix(), "600", 1, unit("vic-anim", 20, 5)))
	if r := f.row(t, "2026-11-15"); r.UnplannedRestarts != 0 || r.Tailnet != usage.TailnetUp {
		t.Fatalf("first poll: %+v", r)
	}
	if _, err := os.Stat(f.StatePath); err != nil {
		t.Fatalf("no state saved: %v", err)
	}
}

// A restart at 23:30 found by the 00:30 poll books on the day it happened.
func TestPollRestartOnEventDay(t *testing.T) {
	f := newFixture(t)
	t0 := time.Date(2026, 11, 14, 22, 30, 0, 0, loc)
	f.poll(t, t0, probe(bootA, t0.Unix(), "36000", 1, unit("vic-anim", 100, 0)))
	t1 := t0.Add(2 * time.Hour)
	f.poll(t, t1, probe(bootA, t1.Unix(), "43200", 1, unit("vic-anim", 39600, 0))) // boot+39600 = 23:30 on the 14th
	if r := f.row(t, "2026-11-14"); r.UnplannedRestarts != 1 {
		t.Fatalf("event day: %+v", r)
	}
	if r := f.row(t, "2026-11-15"); r.UnplannedRestarts != 0 || r.Tailnet != usage.TailnetUp {
		t.Fatalf("poll day: %+v", r)
	}
}

func TestPollShipActiveHoldsTheDiff(t *testing.T) {
	f := newFixture(t)
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, loc)
	f.poll(t, t0, probe(bootA, t0.Unix(), "36000", 1, unit("vic-anim", 100, 0)))
	state0, _ := os.ReadFile(f.StatePath)

	// The ship started at 10:50 and restarted vic-anim at 10:55; the 11:00 poll runs while it is still going.
	start := t0.Add(50 * time.Minute).Unix()
	f.write(t, "ship-active", fmt.Sprintf("%d\n", start))
	t1 := t0.Add(time.Hour)
	f.poll(t, t1, probe(bootA, t1.Unix(), "39600", 1, unit("vic-anim", 39300, 0)))
	if r := f.row(t, "2026-11-15"); r.UnplannedRestarts != 0 {
		t.Fatalf("during the ship: %+v", r)
	}
	if state1, _ := os.ReadFile(f.StatePath); string(state1) != string(state0) {
		t.Fatalf("snapshot advanced during the ship:\n%s\nwas\n%s", state1, state0)
	}

	// The ship ended at 11:10: window logged, marker gone; the 12:00 poll diffs from 10:00 -> exempt.
	f.write(t, "ship-windows.tsv", fmt.Sprintf("%d %d\n", start, t0.Add(70*time.Minute).Unix()))
	if err := os.Remove(f.ActivePath); err != nil {
		t.Fatal(err)
	}
	t2 := t1.Add(time.Hour)
	f.poll(t, t2, probe(bootA, t2.Unix(), "43200", 1, unit("vic-anim", 39300, 0)))
	if r := f.row(t, "2026-11-15"); r.UnplannedRestarts != 0 {
		t.Fatalf("after the window was logged: %+v", r)
	}
	if state2, _ := os.ReadFile(f.StatePath); string(state2) == string(state0) {
		t.Fatal("snapshot not advanced after the ship")
	}
}

// A marker older than ActiveMaxAge (a ship killed before its trap) no longer holds the diff.
func TestPollStaleShipActiveIgnored(t *testing.T) {
	f := newFixture(t)
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, loc)
	f.poll(t, t0, probe(bootA, t0.Unix(), "36000", 1, unit("vic-anim", 100, 0)))
	f.write(t, "ship-active", fmt.Sprintf("%d\n", t0.Add(-4*time.Hour).Unix()))
	t1 := t0.Add(time.Hour)
	f.poll(t, t1, probe(bootA, t1.Unix(), "39600", 1, unit("vic-anim", 39300, 0)))
	if r := f.row(t, "2026-11-15"); r.UnplannedRestarts != 1 {
		t.Fatalf("stale marker: %+v", r)
	}
}

func TestPollTailnetDownSticky(t *testing.T) {
	f := newFixture(t)
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, loc)
	f.poll(t, t0, probe(bootA, t0.Unix(), "36000", 0, unit("vic-anim", 100, 0)))
	if r := f.row(t, "2026-11-15"); r.Tailnet != usage.TailnetDown {
		t.Fatalf("down poll: %+v", r)
	}
	t1 := t0.Add(time.Hour)
	f.poll(t, t1, probe(bootA, t1.Unix(), "39600", 1, unit("vic-anim", 100, 0)))
	if r := f.row(t, "2026-11-15"); r.Tailnet != usage.TailnetDown {
		t.Fatalf("up after down, same day: %+v", r)
	}
	t2 := time.Date(2026, 11, 16, 10, 0, 0, 0, loc)
	f.poll(t, t2, probe(bootA, t2.Unix(), "122400", 1, unit("vic-anim", 100, 0)))
	if r := f.row(t, "2026-11-16"); r.Tailnet != usage.TailnetUp {
		t.Fatalf("next day up: %+v", r)
	}
}

func TestPollFailureWritesNothing(t *testing.T) {
	f := newFixture(t)
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, loc)
	f.poll(t, t0, probe(bootA, t0.Unix(), "36000", 1, unit("vic-anim", 100, 0)))
	ledger0, _ := os.ReadFile(f.Ledger.Path)
	state0, _ := os.ReadFile(f.StatePath)
	t1 := t0.Add(time.Hour)
	for name, set := range map[string]func(){
		"ssh failed": func() { f.out, f.err = "", errors.New("robot probe: exit 255") },
		"truncated":  func() { f.out = strings.TrimSuffix(probe(bootB, t1.Unix(), "60", 0, unit("vic-anim", 5, 0)), "end\n") },
	} {
		f.now, f.out, f.err = t1, "", nil
		set()
		if err := f.Poll(context.Background()); err == nil {
			t.Fatalf("%s: no error", name)
		}
		ledger1, _ := os.ReadFile(f.Ledger.Path)
		state1, _ := os.ReadFile(f.StatePath)
		if string(ledger1) != string(ledger0) || string(state1) != string(state0) {
			t.Fatalf("%s: wrote\n%s\n%s", name, ledger1, state1)
		}
	}
}

// The state file carries nothing but boot_id, clock/uptime and vic-* counters.
func TestStateHasNoTailnetOrAddress(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, loc)
	f.poll(t, now, probe(bootA, now.Unix(), "600", 1, unit("vic-anim", 20, 0)))
	b, _ := os.ReadFile(f.StatePath)
	want := fmt.Sprintf(`{"boot_id":"%s","now":%d,"uptime_us":600000000,"units":{"vic-anim":{"enter_us":20000000,"n_restarts":0}}}`+"\n", bootA, now.Unix())
	if string(b) != want {
		t.Fatalf("state\n%s\nwant\n%s", b, want)
	}
}
