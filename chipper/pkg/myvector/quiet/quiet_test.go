package quiet

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMatchTable(t *testing.T) {
	hits := []string{"shut up", "Shut up!", "turn off", "vector, shut up please", "hey vector turn off",
		"ole vait", "Jää vait.", "palun lülita välja", "Hei Vektor, lülitu välja!", "shutup"}
	misses := []string{"turn off the kitchen light", "don't turn off the music", "shut up and dance",
		"lülita tuled välja", "ära ole vait", "vaikselt", "vector", "", "what time is it"}
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

func TestMatchAny(t *testing.T) {
	if p, ok := MatchAny("the forecast says rain, shut up vector, and wind"); !ok || p != "shut up" {
		t.Errorf("MatchAny mid-sentence = %q %v, want shut up", p, ok)
	}
	for _, s := range []string{"shutters upstairs", "the turnoff is left", "vaikselt räägib"} {
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
	go func() { done <- Nap("x", time.Now().Add(time.Hour)) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Nap = nil against a hung console, want a timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Nap ignored NapTimeout")
	}
	t.Setenv("MYVECTOR_NAP", "off")
	if err := Nap("x", time.Time{}); !errors.Is(err, ErrNapOff) {
		t.Errorf("MYVECTOR_NAP=off: Nap = %v, want ErrNapOff", err)
	}
}

func TestNapValueAndStatus(t *testing.T) {
	t.Setenv("MYVECTOR_NAP", "")
	var got []string
	code := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Query().Get("value"))
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	old := NapURL
	NapURL = func(string) string { return srv.URL }
	t.Cleanup(func() { NapURL = old })
	if err := Nap("x", time.Time{}); err != nil || len(got) != 1 || got[0] != "0" {
		t.Errorf("Nap(zero) = %v, sent %v; want value=0 (nap off)", err, got)
	}
	code = http.StatusInternalServerError
	if err := Nap("x", time.Unix(1700000000, 0)); err == nil || got[1] != "1700000000" {
		t.Errorf("Nap on a 500 = %v, sent %v; want an error and value in epoch seconds", err, got)
	}
}
