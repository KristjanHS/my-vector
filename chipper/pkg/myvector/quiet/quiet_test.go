package quiet

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestMatchTable(t *testing.T) {
	hits := []string{"shut up", "Shut up!", "turn off", "vector, shut up please", "hey vector turn off",
		"ole vait", "Jää vait.", "palun lülita välja", "Hei Vektor, lülitu välja!", "shutup",
		"Jäävait.", "Lülituvälja.", "Vektor, ole vaid palun.", "vaid."} // TTS→STT round-trip spellings
	misses := []string{"turn off the kitchen light", "don't turn off the music", "shut up and dance",
		"lülita tuled välja", "ära ole vait", "vaikselt", "vector", "", "what time is it",
		"mitte tema, vaid sina"}
	for _, s := range hits {
		if _, ok := Match(s); !ok {
			t.Errorf("Match(%q) = miss, want hit", s)
		}
	}
	for _, s := range misses {
		if p, ok := Match(s); ok {
			t.Errorf("Match(%q) = hit %q, want miss", s, p)
		}
	}
}

func TestMatchExact(t *testing.T) {
	if _, ok := MatchExact("Vektor, ole vait!"); !ok {
		t.Error("MatchExact(ole vait) = miss, want hit")
	}
	for _, s := range []string{"Jäävait.", "Vaid?", "ole vaid", "Turnoff."} {
		if p, ok := MatchExact(s); ok {
			t.Errorf("MatchExact(%q) = hit %q, want miss (Match-only spelling)", s, p)
		}
	}
}

func TestMatchAny(t *testing.T) {
	if p, ok := MatchAny("the forecast says rain, shut up vector, and wind"); !ok || p != "shut up" {
		t.Errorf("MatchAny mid-sentence = %q %v, want shut up", p, ok)
	}
	for _, s := range []string{"shutters upstairs", "the turnoff is left", "vaikselt räägib", "mitte tema, vaid sina"} {
		if p, ok := MatchAny(s); ok {
			t.Errorf("MatchAny(%q) = hit %q, want miss (word boundary)", s, p)
		}
	}
}

func lockFile(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "quiet-until")
	t.Setenv("MYVECTOR_QUIET_FILE", p)
	return p
}

func TestSetExtendsNeverShortens(t *testing.T) {
	lockFile(t)
	now := time.Unix(time.Now().Unix(), 0)
	if u, _, err := Until(); err != nil || !u.IsZero() {
		t.Fatalf("no file: Until = %v %v, want zero", u, err)
	}
	if err := Set(now.Add(LockFor), "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	if err := Set(now.Add(time.Minute), "make quiet"); err != nil {
		t.Fatal(err)
	}
	u, src, _ := Until()
	if !u.Equal(now.Add(LockFor)) || src != "voice:shut up" {
		t.Fatalf("after shorter Set: %v %q, want %v voice:shut up", u, src, now.Add(LockFor))
	}
	if err := Set(now.Add(3*time.Hour), "make quiet"); err != nil {
		t.Fatal(err)
	}
	if u, src, _ = Until(); !u.Equal(now.Add(3*time.Hour)) || src != "make quiet" {
		t.Fatalf("after longer Set: %v %q", u, src)
	}
}

func TestSetAtomic(t *testing.T) {
	p := lockFile(t)
	base := time.Now()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, _, err := Until(); err != nil {
				t.Errorf("reader saw a partial file: %v", err)
				return
			}
		}
	}()
	for i := 1; i <= 300; i++ {
		if err := Set(base.Add(time.Duration(i)*time.Second), "voice:shut up"); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	ents, _ := os.ReadDir(filepath.Dir(p))
	if len(ents) != 1 {
		t.Fatalf("dir holds %d entries, want only quiet-until (no tmp left)", len(ents))
	}
}

func TestUntilRejectsGarbage(t *testing.T) {
	p := lockFile(t)
	os.WriteFile(p, []byte("soon\n"), 0o644)
	if _, _, err := Until(); err == nil {
		t.Fatal("garbage lock file parsed without error")
	}
}

func TestNapTimeout(t *testing.T) {
	t.Setenv("MYVECTOR_NAP", "")
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); srv.Close() })
	oldURL, oldT := NapURL, NapTimeout
	NapURL, NapTimeout = func(string) string { return srv.URL }, 50*time.Millisecond
	t.Cleanup(func() { NapURL, NapTimeout = oldURL, oldT })
	done := make(chan error, 1)
	go func() { _, err := Nap("x", time.Now().Add(time.Hour)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Nap = nil against a hung console, want a timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Nap ignored NapTimeout")
	}
	t.Setenv("MYVECTOR_NAP", "off")
	if _, err := Nap("x", time.Time{}); !errors.Is(err, ErrNapOff) {
		t.Errorf("MYVECTOR_NAP=off: Nap = %v, want ErrNapOff", err)
	}
}

func TestNapValueAndStatus(t *testing.T) {
	t.Setenv("MYVECTOR_NAP", "")
	var got []string
	code := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("value")
		got = append(got, v)
		w.WriteHeader(code)
		fmt.Fprintf(w, "%s<br>\n", v) // vic-engine's body for a set: the var's new value
	}))
	t.Cleanup(srv.Close)
	old := NapURL
	NapURL = func(string) string { return srv.URL }
	t.Cleanup(func() { NapURL = old })
	if _, err := Nap("x", time.Time{}); err != nil || len(got) != 1 || got[0] != "0" {
		t.Errorf("Nap(zero) = %v, sent %v; want value=0 (nap off)", err, got)
	}
	code = http.StatusInternalServerError
	if _, err := Nap("x", time.Unix(1700000000, 0)); err == nil || got[1] != "1700000000" {
		t.Errorf("Nap on a 500 = %v, sent %v; want an error and value in epoch seconds", err, got)
	}
}

// vic-engine answers 200 with "Variable not found" (a firmware without the nap) or "Error setting variable": not a set.
func TestNapBodyNotSet(t *testing.T) {
	t.Setenv("MYVECTOR_NAP", "")
	body := "Variable not found SleepCycle_NapUntil_s<br>\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	t.Cleanup(srv.Close)
	old := NapURL
	NapURL = func(string) string { return srv.URL }
	t.Cleanup(func() { NapURL = old })
	if _, err := Nap("x", time.Now().Add(time.Hour)); err == nil {
		t.Errorf("Nap on a 200 %q = nil, want an error", body)
	}
	body = "Error setting variable SleepCycle_NapUntil_s=0<br>\n"
	if _, err := Nap("x", time.Time{}); err == nil {
		t.Errorf("Nap on a 200 %q = nil, want an error", body)
	}
}

// A lock past NapMax: the nap is capped at now+NapMax-NapMargin (patch 0011 refuses past NapMax on the robot clock).
func TestNapCapsAtNapMax(t *testing.T) {
	t.Setenv("MYVECTOR_NAP", "")
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("value")
		fmt.Fprintf(w, "%s<br>\n", got)
	}))
	t.Cleanup(srv.Close)
	old := NapURL
	NapURL = func(string) string { return srv.URL }
	t.Cleanup(func() { NapURL = old })
	before := time.Now()
	sent, err := Nap("x", before.Add(6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sec, _ := strconv.ParseInt(got, 10, 64)
	if want := before.Add(NapMax - NapMargin).Unix(); sec < want || sec > want+2 {
		t.Errorf("Nap(now+6h) sent value=%s, want now+NapMax-NapMargin (%d)", got, want)
	}
	if sent.Unix() != sec {
		t.Errorf("Nap returned %d, sent %d: want the value sent", sent.Unix(), sec)
	}
}

func TestLine(t *testing.T) {
	p := lockFile(t)
	now := time.Now()
	if line, ok := Line(now); ok || line != "" {
		t.Fatalf("no file: Line = %q %v, want unlocked", line, ok)
	}
	until := time.Date(2026, 11, 7, 23, 5, 0, 0, time.Local)
	if err := Set(until, "make quiet"); err != nil {
		t.Fatal(err)
	}
	want := "QUIET: robot sound locked until 23:05 (make quiet); make unquiet lifts it"
	if line, ok := Line(until.Add(-time.Minute)); !ok || line != want {
		t.Errorf("locked: Line = %q %v, want %q", line, ok, want)
	}
	if _, ok := Line(until); ok {
		t.Error("Line at until reads locked, want expired")
	}
	if err := os.WriteFile(p, []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Line(until.Add(-time.Minute)); ok {
		t.Error("garbage lock file reads locked, want fail-open")
	}
}

func TestSetKeepsFirstVolume(t *testing.T) {
	p := lockFile(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if err := SetVol(now.Add(time.Hour), 4, "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != fmt.Sprintf("%d 4 voice:shut up\n", now.Add(time.Hour).Unix()) {
		t.Fatalf("file %q, want `<until> 4 voice:shut up`", b)
	}
	if err := SetVol(now.Add(2*time.Hour), LockVolume, "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	if l, _ := Read(); !l.Until.Equal(now.Add(2*time.Hour)) || l.Vol != 4 {
		t.Fatalf("extend: %+v, want until +2h, vol 4 (first saved)", l)
	}
	if err := SetVol(now.Add(time.Minute), 2, "cli:quiet.sh"); err != nil {
		t.Fatal(err)
	}
	if l, _ := Read(); !l.Until.Equal(now.Add(2*time.Hour)) || l.Vol != 4 || l.Source != "voice:shut up" {
		t.Fatalf("shorter: %+v, want unchanged", l)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	if err := Set(now.Add(time.Hour), "cli:quiet.sh"); err != nil {
		t.Fatal(err)
	}
	if err := SetVol(now.Add(time.Hour), 3, "cli:quiet.sh"); err != nil {
		t.Fatal(err)
	}
	if l, _ := Read(); l.Vol != 3 || !l.Until.Equal(now.Add(time.Hour)) {
		t.Fatalf("a `-` lock: %+v, want the first known volume 3 filled in", l)
	}
	if err := SetVol(now.Add(time.Hour), MaxVolume+1, "x"); err == nil {
		t.Error("SetVol accepted a volume above MaxVolume")
	}
}

// TestReadLockForms: the old 2-field file still parses; the split matches guard.sh quiet_locked.
func TestReadLockForms(t *testing.T) {
	p := lockFile(t)
	for _, c := range []struct {
		line string
		vol  int
		src  string
	}{
		{"1794000000 voice:shut up", NoVolume, "voice:shut up"},
		{"1794000000 make quiet", NoVolume, "make quiet"},
		{"1794000000 4 voice:shut up", 4, "voice:shut up"},
		{"1794000000 - cli:quiet.sh", NoVolume, "cli:quiet.sh"},
		{"1794000000 0 cli:quiet.sh", 0, "cli:quiet.sh"},
		{"1794000000 4", NoVolume, "4"},
		{"1794000000 9 voice:shut up", NoVolume, "voice:shut up"},
	} {
		if err := os.WriteFile(p, []byte(c.line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		l, err := Read()
		if err != nil || l.Until.Unix() != 1794000000 || l.Vol != c.vol || l.Source != c.src {
			t.Errorf("%q: %+v %v, want vol %d source %q", c.line, l, err, c.vol, c.src)
		}
	}
}

// TestHoldWritesAsIs: Hold shortens (no max) and keeps the volume; the expired file reads unlocked.
func TestHoldWritesAsIs(t *testing.T) {
	lockFile(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if err := SetVol(now.Add(time.Hour), 4, "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	if err := Hold(Lock{Until: now, Vol: 4, Source: "voice:shut up"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := Read(); !l.Until.Equal(now) || l.Vol != 4 || l.Source != "voice:shut up" {
		t.Fatalf("after Hold: %+v, want until now, vol 4", l)
	}
	if _, locked := Line(now); locked {
		t.Error("held expired file reads locked")
	}
	if err := Hold(Lock{Until: now, Vol: MaxVolume + 1}); err == nil {
		t.Error("Hold accepted a volume above MaxVolume")
	}
}

// TestSetVolRace: Sets racing a SetVol never write the saved volume back to `-` (I2: the next read would save LockVolume).
func TestSetVolRace(t *testing.T) {
	lockFile(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 200; i++ {
		if err := Clear(); err != nil {
			t.Fatal(err)
		}
		if err := Set(now.Add(time.Hour), "voice:shut up"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(4)
		go func() { defer wg.Done(); _ = SetVol(now.Add(time.Hour), 4, "voice:shut up") }()
		for j := 1; j <= 3; j++ {
			go func(j int) {
				defer wg.Done()
				_ = Set(now.Add(time.Hour+time.Duration(j)*time.Minute), "voice:shut up")
			}(j)
		}
		wg.Wait()
		if l, err := Read(); err != nil || l.Vol != 4 || !l.Until.Equal(now.Add(time.Hour+3*time.Minute)) {
			t.Fatalf("round %d: %+v %v, want until +1h3m vol 4", i, l, err)
		}
	}
}

// TestClearIfHoldIf: a lock that changed since it was read is left as is; an unchanged one is cleared/held.
func TestClearIfHoldIf(t *testing.T) {
	lockFile(t)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if err := SetVol(now, 4, "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	was, _ := Read()
	if err := Set(now.Add(2*time.Hour), "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	if ok, err := ClearIf(was); ok || err != nil {
		t.Fatalf("ClearIf over a newer lock = %v %v, want false", ok, err)
	}
	if ok, err := HoldIf(was, Lock{Until: now, Vol: 4, Source: "voice:shut up"}); ok || err != nil {
		t.Fatalf("HoldIf over a newer lock = %v %v, want false", ok, err)
	}
	if l, _ := Read(); !l.Until.Equal(now.Add(2*time.Hour)) || l.Vol != 4 {
		t.Fatalf("newer lock %+v, want until +2h vol 4 untouched", l)
	}
	cur, _ := Read()
	if ok, err := HoldIf(cur, Lock{Until: now, Vol: 4, Source: "voice:shut up"}); !ok || err != nil {
		t.Fatalf("HoldIf unchanged = %v %v, want true", ok, err)
	}
	cur, _ = Read()
	if ok, err := ClearIf(cur); !ok || err != nil {
		t.Fatalf("ClearIf unchanged = %v %v, want true", ok, err)
	}
	if l, _ := Read(); !l.Until.IsZero() {
		t.Fatalf("after ClearIf: %+v, want the file gone", l)
	}
}
