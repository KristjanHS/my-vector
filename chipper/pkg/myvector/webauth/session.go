package webauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const (
	CookieName   = "mv_session"
	SessionTTL   = 14 * 24 * time.Hour
	SessionRenew = 7 * 24 * time.Hour // re-issue when less than this is left
)

// Issue returns a cookie value `exp.nonce.mac`, mac = HMAC-SHA256(SessionKey, "exp.nonce").
func (s *Store) Issue(now time.Time) string {
	body := strconv.FormatInt(now.Add(SessionTTL).Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(random(16))
	return body + "." + s.mac(body)
}

// Verify reports whether v is an unexpired session this store issued, and its expiry.
func (s *Store) Verify(v string, now time.Time) (time.Time, bool) {
	i := strings.LastIndexByte(v, '.')
	if i < 0 {
		return time.Time{}, false
	}
	body, mac := v[:i], v[i+1:]
	if !hmac.Equal([]byte(mac), []byte(s.mac(body))) {
		return time.Time{}, false
	}
	expStr, _, _ := strings.Cut(body, ".")
	sec, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	exp := time.Unix(sec, 0)
	if !now.Before(exp) {
		return time.Time{}, false
	}
	return exp, true
}

func (s *Store) mac(body string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
