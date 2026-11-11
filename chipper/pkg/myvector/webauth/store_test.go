package webauth

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func testPaths(t *testing.T) Paths {
	d := t.TempDir()
	return Paths{Auth: filepath.Join(d, "server", "webauth.json"), Password: filepath.Join(d, "webui-password"),
		Header: filepath.Join(d, "wirepod-auth.header")}
}

func captureLog(t *testing.T) *strings.Builder {
	var b strings.Builder
	old := logln
	logln = func(a ...any) { fmt.Fprintln(&b, a...) }
	t.Cleanup(func() { logln = old })
	return &b
}

func mustRead(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStoreBootstrap(t *testing.T) {
	p := testPaths(t)
	log := captureLog(t)
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p.Auth, p.Password, p.Header} {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", f, st.Mode().Perm())
		}
	}
	if st, _ := os.Stat(filepath.Dir(p.Auth)); st.Mode().Perm() != 0o700 {
		t.Errorf("created dir mode %v, want 0700", st.Mode().Perm())
	}
	pw := strings.TrimSuffix(mustRead(t, p.Password), "\n")
	if !regexp.MustCompile(`^[a-z2-7]{26}$`).MatchString(pw) {
		t.Errorf("password %q: want 26 base32 chars", pw)
	}
	hdr := mustRead(t, p.Header)
	m := regexp.MustCompile(`^Authorization: Bearer ([A-Za-z0-9_-]{43})\n$`).FindStringSubmatch(hdr)
	if m == nil {
		t.Fatalf("header line %q", hdr)
	}
	if cost, err := bcrypt.Cost(s.hash); err != nil || cost != BcryptCost {
		t.Errorf("bcrypt cost %d (%v), want %d", cost, err, BcryptCost)
	}
	if !s.CheckPassword(pw) || !s.CheckToken(m[1]) {
		t.Error("bootstrapped password/token rejected")
	}
	if len(s.key) != 32 {
		t.Errorf("session key %d bytes", len(s.key))
	}
	out := log.String()
	for _, f := range []string{p.Auth, p.Password, p.Header} {
		if !strings.Contains(out, f) {
			t.Errorf("log lacks path %s: %q", f, out)
		}
	}
	if strings.Contains(out, pw) || strings.Contains(out, m[1]) {
		t.Errorf("log leaks a secret: %q", out)
	}
	// Second start: reuse, never regenerate.
	s2, err := Load(p)
	if err != nil || !s2.CheckPassword(pw) || mustRead(t, p.Header) != hdr {
		t.Errorf("reload regenerated or failed: %v", err)
	}
}

func TestStoreCorruptFailsClosed(t *testing.T) {
	for name, body := range map[string]string{
		"not json":    "{not json",
		"empty":       "",
		"no fields":   "{}",
		"short token": `{"password_bcrypt":"$2a$12$abcdefghijklmnopqrstuuJ5c9zv2b0JzZ4w8kq3o0X0n8wDrhRa6","token_sha256":"ab","session_key":"AAAA"}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := testPaths(t)
			os.MkdirAll(filepath.Dir(p.Auth), 0o700)
			os.WriteFile(p.Auth, []byte(body), 0o600)
			if s, err := Load(p); err == nil || s != nil {
				t.Fatalf("Load = %v, %v; want error", s, err)
			}
			if mustRead(t, p.Auth) != body {
				t.Error("corrupt store was overwritten")
			}
			if _, err := os.Stat(p.Password); !os.IsNotExist(err) {
				t.Error("password file regenerated over a corrupt store")
			}
		})
	}
}

func TestCheckToken(t *testing.T) {
	p := testPaths(t)
	captureLog(t)
	s, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	tok := strings.TrimSuffix(strings.TrimPrefix(mustRead(t, p.Header), "Authorization: Bearer "), "\n")
	for _, c := range []struct {
		tok  string
		want bool
	}{{tok, true}, {tok + "x", false}, {tok[1:], false}, {"", false}} {
		if got := s.CheckToken(c.tok); got != c.want {
			t.Errorf("CheckToken(%q) = %v", c.tok, got)
		}
	}
	if s.CheckPassword("") {
		t.Error("empty password accepted")
	}
}

func TestSessionExpiry(t *testing.T) {
	s := &Store{key: random(32)}
	t0 := time.Unix(1_800_000_000, 0)
	v := s.Issue(t0)
	if exp, ok := s.Verify(v, t0.Add(SessionTTL-time.Second)); !ok || !exp.Equal(t0.Add(SessionTTL)) {
		t.Errorf("fresh session: ok=%v exp=%v", ok, exp)
	}
	if _, ok := s.Verify(v, t0.Add(SessionTTL)); ok {
		t.Error("session valid at its expiry")
	}
}

func TestSessionTamper(t *testing.T) {
	s := &Store{key: random(32)}
	now := time.Unix(1_800_000_000, 0)
	v := s.Issue(now)
	b := []byte(v)
	last := len(b) - 1
	if b[last] == 'A' {
		b[last] = 'B'
	} else {
		b[last] = 'A'
	}
	exp, _, _ := strings.Cut(v, ".")
	later := fmt.Sprint(now.Add(100*SessionTTL).Unix()) + strings.TrimPrefix(v, exp)
	for name, bad := range map[string]string{
		"mac byte":    string(b),
		"exp swapped": later,
		"no dots":     "abc",
		"empty":       "",
		"other key":   (&Store{key: random(32)}).Issue(now),
	} {
		if _, ok := s.Verify(bad, now); ok {
			t.Errorf("%s: tampered session verified", name)
		}
	}
}
