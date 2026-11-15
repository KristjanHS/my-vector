package usage

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"
)

var (
	robot = net.ParseIP("192.168.1.50")
	day1  = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	day2  = time.Date(2026, 11, 21, 23, 59, 0, 0, time.UTC)
)

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		name string
		peer net.IP
		text string
		want Kind
	}{
		{"loopback v4 with text", net.ParseIP("127.0.0.1"), "what time is it", Test},
		{"loopback v6", net.ParseIP("::1"), "hello", Test},
		{"loopback v4-mapped v6", net.ParseIP("::ffff:127.0.0.1"), "hello", Test},
		{"loopback empty text", net.ParseIP("127.0.0.1"), "", Test},
		{"nil peer with text", nil, "hello", Test},
		{"robot with text", robot, "what time is it", Real},
		{"tailnet robot with text", net.ParseIP("100.64.0.7"), "hello", Real},
		{"robot empty text", robot, "", None},
		{"robot whitespace text", robot, " \t\n", None},
	} {
		if got := Classify(c.peer, c.text); got != c.want {
			t.Errorf("%s: Classify = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAddrIP(t *testing.T) {
	if ip := AddrIP(&net.TCPAddr{IP: robot, Port: 51000}); !ip.Equal(robot) {
		t.Errorf("TCPAddr -> %v", ip)
	}
	if ip := AddrIP(&net.UnixAddr{Name: "/run/x.sock", Net: "unix"}); ip != nil {
		t.Errorf("unix addr -> %v, want nil", ip)
	}
	if ip := AddrIP(nil); ip != nil {
		t.Errorf("nil addr -> %v, want nil", ip)
	}
}

func ledger(t *testing.T) *Ledger { return Open(filepath.Join(t.TempDir(), "usage.tsv")) }

func rows(t *testing.T, l *Ledger) []Row {
	t.Helper()
	r, err := l.Rows()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCountsOneDayMergeIntoOneRow(t *testing.T) {
	l := ledger(t)
	for _, k := range []Kind{Real, Real, Test, None} {
		if err := l.Count(k, day1); err != nil {
			t.Fatal(err)
		}
	}
	want := []Row{{Day: "2026-11-20", Real: 2, Test: 1, Tailnet: "-"}}
	if got := rows(t, l); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func TestNewDayAppendsRow(t *testing.T) {
	l := ledger(t)
	l.Count(Real, day1)
	if err := l.Count(Test, day2); err != nil {
		t.Fatal(err)
	}
	want := []Row{{Day: "2026-11-20", Real: 1, Tailnet: "-"}, {Day: "2026-11-21", Test: 1, Tailnet: "-"}}
	if got := rows(t, l); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
	b, _ := os.ReadFile(l.Path)
	if s := string(b); s != Header+"\n2026-11-20\t1\t0\t0\t-\n2026-11-21\t0\t1\t0\t-\n" {
		t.Errorf("file = %q", s)
	}
}

func TestUpdateKeepsCounters(t *testing.T) {
	l := ledger(t)
	l.Count(Real, day1)
	l.Count(Test, day1)
	if err := l.Update("2026-11-20", func(r *Row) { r.UnplannedRestarts++; r.Tailnet = TailnetUp }); err != nil {
		t.Fatal(err)
	}
	l.Count(Real, day1)
	want := []Row{{Day: "2026-11-20", Real: 2, Test: 1, UnplannedRestarts: 1, Tailnet: "up"}}
	if got := rows(t, l); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func TestReloadAfterFreshInstanceKeepsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.tsv")
	Open(path).Count(Real, day1)
	Open(path).Count(Real, day1)
	l := Open(path) // a restarted wire-pod
	if err := l.Count(Test, day1); err != nil {
		t.Fatal(err)
	}
	want := []Row{{Day: "2026-11-20", Real: 2, Test: 1, Tailnet: "-"}}
	if got := rows(t, Open(path)); !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

var dottedQuad = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+`)

func TestFileHoldsNoIP(t *testing.T) {
	l := ledger(t)
	for _, p := range []net.IP{robot, net.ParseIP("127.0.0.1"), net.ParseIP("100.64.0.7")} {
		if err := l.Count(Classify(p, "hello"), day1); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Update("2026-11-20", func(r *Row) { r.Tailnet = "100.64.0.7" }); err == nil {
		t.Error("Update accepted an IP as the tailnet value")
	}
	b, err := os.ReadFile(l.Path)
	if err != nil {
		t.Fatal(err)
	}
	if m := dottedQuad.Find(b); m != nil {
		t.Errorf("ledger holds a dotted quad %q:\n%s", m, b)
	}
	if want := Header + "\n2026-11-20\t2\t1\t0\t-\n"; string(b) != want {
		t.Errorf("file = %q, want %q", b, want)
	}
}

func TestMalformedLedgerIsNotOverwritten(t *testing.T) {
	l := ledger(t)
	bad := Header + "\n2026-11-20\t1\tx\t0\t-\n"
	os.WriteFile(l.Path, []byte(bad), 0o600)
	if err := l.Count(Real, day1); err == nil {
		t.Error("Count on a malformed ledger succeeded")
	}
	if b, _ := os.ReadFile(l.Path); string(b) != bad {
		t.Errorf("malformed ledger was rewritten: %q", b)
	}
}
