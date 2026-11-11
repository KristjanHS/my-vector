// Package webauth guards wire-pod's web UI/API on :80 and :8080 (plan 2026-11-11-webui-auth.md (docs/plans/)).
package webauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"golang.org/x/crypto/bcrypt"
)

const BcryptCost = 12

var logln = logger.Println

// Paths are the three secret files; Auth is written last, so its presence means a complete bootstrap.
type Paths struct{ Auth, Password, Header string }

type storeFile struct {
	PasswordBcrypt string `json:"password_bcrypt"`
	TokenSHA256    string `json:"token_sha256"`
	SessionKey     string `json:"session_key"`
}

type Store struct {
	hash     []byte
	tokenSum []byte
	key      []byte
}

// Load reads p.Auth, bootstrapping all three files when it is absent. A present but unusable file is an
// error, never regenerated over (the caller fails closed).
func Load(p Paths) (*Store, error) {
	b, err := os.ReadFile(p.Auth)
	if errors.Is(err, os.ErrNotExist) {
		return bootstrap(p)
	}
	if err != nil {
		return nil, fmt.Errorf("webauth: read %s: %w", p.Auth, err)
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("webauth: parse %s: %w", p.Auth, err)
	}
	s, err := f.store()
	if err != nil {
		return nil, fmt.Errorf("webauth: %s: %w", p.Auth, err)
	}
	return s, nil
}

func (f storeFile) store() (*Store, error) {
	if _, err := bcrypt.Cost([]byte(f.PasswordBcrypt)); err != nil {
		return nil, fmt.Errorf("password_bcrypt: %w", err)
	}
	sum, err := hex.DecodeString(f.TokenSHA256)
	if err != nil || len(sum) != sha256.Size {
		return nil, errors.New("token_sha256: want 64 hex chars")
	}
	key, err := base64.RawURLEncoding.DecodeString(f.SessionKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("session_key: want 32 bytes base64url")
	}
	return &Store{hash: []byte(f.PasswordBcrypt), tokenSum: sum, key: key}, nil
}

func bootstrap(p Paths) (*Store, error) {
	pw := strings.ToLower(base32.StdEncoding.EncodeToString(random(17)))[:26]
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), BcryptCost)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(tok))
	f := storeFile{PasswordBcrypt: string(hash), TokenSHA256: hex.EncodeToString(sum[:]),
		SessionKey: base64.RawURLEncoding.EncodeToString(random(32))}
	js, _ := json.MarshalIndent(f, "", "  ")
	for _, w := range []struct{ path, body string }{
		{p.Password, pw + "\n"},
		{p.Header, "Authorization: Bearer " + tok + "\n"},
		{p.Auth, string(js) + "\n"},
	} {
		if err := writeSecret(w.path, w.body); err != nil {
			return nil, err
		}
	}
	logln("webauth: new web-UI credentials — password in", p.Password, "script header in", p.Header, "store", p.Auth)
	return f.store()
}

func writeSecret(path, body string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	t, err := os.CreateTemp(dir, ".webauth-*")
	if err != nil {
		return err
	}
	defer os.Remove(t.Name())
	if _, err := t.WriteString(body); err != nil {
		t.Close()
		return err
	}
	if err := t.Chmod(0o600); err != nil {
		t.Close()
		return err
	}
	if err := t.Close(); err != nil {
		return err
	}
	return os.Rename(t.Name(), path)
}

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("webauth: crypto/rand: " + err.Error())
	}
	return b
}

func (s *Store) CheckPassword(pw string) bool {
	return pw != "" && bcrypt.CompareHashAndPassword(s.hash, []byte(pw)) == nil
}

func (s *Store) CheckToken(tok string) bool {
	if tok == "" {
		return false
	}
	sum := sha256.Sum256([]byte(tok))
	return subtle.ConstantTimeCompare(sum[:], s.tokenSum) == 1
}
