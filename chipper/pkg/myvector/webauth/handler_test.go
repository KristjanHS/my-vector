package webauth

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const testPW, testTok = "correct horse", "tok-123"

// testGuard: a stub mux behind the guard; bcrypt at MinCost keeps the rate-limit rows fast.
func testGuard(t *testing.T) (*guard, *Store) {
	h, _ := bcrypt.GenerateFromPassword([]byte(testPW), bcrypt.MinCost)
	s := &Store{hash: h, key: random(32)}
	sum := sha256.Sum256([]byte(testTok))
	s.tokenSum = sum[:]
	stub := http.NewServeMux()
	stub.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Write([]byte("stub"))
	})
	stub.HandleFunc("/api/nowrite", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
	})
	return newGuard(s, stub), s
}

type req struct {
	method, target string
	hdr            map[string]string
	body           string
	remote         string
}

func (g *guard) do(q req) *httptest.ResponseRecorder {
	if q.method == "" {
		q.method = http.MethodGet
	}
	r := httptest.NewRequest(q.method, "http://localhost:8080"+q.target, strings.NewReader(q.body))
	r.RemoteAddr = "127.0.0.1:5000"
	if q.remote != "" {
		r.RemoteAddr = q.remote
	}
	if q.body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range q.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

func TestHandlerTable(t *testing.T) {
	g, s := testGuard(t)
	cookie := CookieName + "=" + s.Issue(time.Now())
	bearer := "Bearer " + testTok
	for _, c := range []struct {
		name string
		q    req
		code int
		loc  string // Location header, when set
	}{
		{"no creds from loopback (D8)", req{target: "/api/get_config"}, 401, ""},
		{"html GET → login", req{target: "/settings?x=1", hdr: map[string]string{"Accept": "text/html"}}, 303, LoginPath + "?next=" + url.QueryEscape("/settings?x=1")},
		{"html GET //evil → next=/", req{target: "//evil.example/x", hdr: map[string]string{"Accept": "text/html"}}, 303, LoginPath + "?next=%2F"},
		{"bearer", req{target: "/api/get_config", hdr: map[string]string{"Authorization": bearer}}, 200, ""},
		{"wrong bearer", req{target: "/api/get_config", hdr: map[string]string{"Authorization": "Bearer nope"}}, 401, ""},
		{"wrong bearer + good cookie", req{target: "/", hdr: map[string]string{"Authorization": "Bearer nope", "Cookie": cookie}}, 401, ""},
		{"basic scheme", req{target: "/", hdr: map[string]string{"Authorization": "Basic " + testTok}}, 401, ""},
		{"cookie", req{target: "/api/get_config", hdr: map[string]string{"Cookie": cookie}}, 200, ""},
		{"forged cookie", req{target: "/", hdr: map[string]string{"Cookie": CookieName + "=1.2.3"}}, 401, ""},
		{"cookie same-site fetch", req{target: "/", hdr: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "same-site"}}, 403, ""},
		{"cookie cross-site fetch", req{target: "/", hdr: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "cross-site"}}, 403, ""},
		{"cookie same-origin fetch", req{target: "/", hdr: map[string]string{"Cookie": cookie, "Sec-Fetch-Site": "same-origin"}}, 200, ""},
		{"cookie POST foreign Origin", req{method: "POST", target: "/api/set_kg_api", hdr: map[string]string{"Cookie": cookie, "Origin": "http://evil.example"}}, 403, ""},
		{"cookie POST no Origin/Referer", req{method: "POST", target: "/api/set_kg_api", hdr: map[string]string{"Cookie": cookie}}, 403, ""},
		{"cookie POST own Origin", req{method: "POST", target: "/api/set_kg_api", hdr: map[string]string{"Cookie": cookie, "Origin": "http://localhost:8080"}}, 200, ""},
		{"cookie POST own Referer", req{method: "POST", target: "/api/set_kg_api", hdr: map[string]string{"Cookie": cookie, "Referer": "http://localhost:8080/"}}, 200, ""},
		{"bearer POST skips CSRF", req{method: "POST", target: "/api/set_kg_api", hdr: map[string]string{"Authorization": bearer, "Sec-Fetch-Site": "cross-site"}}, 200, ""},
		{"exempt /ok", req{target: "/ok"}, 200, ""},
		{"exempt /ok:80", req{target: "/ok:80"}, 200, ""},
		{"exempt GET get_ota", req{target: "/api/get_ota/x.ota"}, 200, ""},
		{"POST get_ota", req{method: "POST", target: "/api/get_ota/x"}, 401, ""},
		{"get_ota dot-dot", req{target: "/api/get_ota/../get_config"}, 401, ""},
		{"session-certs behind auth (D11)", req{target: "/session-certs/00e20145"}, 401, ""},
		{"login page", req{target: LoginPath}, 200, ""},
	} {
		w := g.do(c.q)
		if w.Code != c.code {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.code)
		}
		if c.loc != "" && w.Header().Get("Location") != c.loc {
			t.Errorf("%s: Location %q, want %q", c.name, w.Header().Get("Location"), c.loc)
		}
		if w.Code == 401 && w.Header().Get("WWW-Authenticate") != `Bearer realm="wire-pod"` {
			t.Errorf("%s: 401 without WWW-Authenticate", c.name)
		}
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{"/a?b=1": "/a?b=1", "//evil": "/", `/\evil`: "/", "http://evil": "/", "": "/", "evil": "/",
		`/./\evil.com`: "/", "/\t/evil.com": "/", "/\n/evil.com": "/", "/\x7f": "/", "/a/..//evil": "/a/..//evil"} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPIDropsCORS(t *testing.T) {
	g, _ := testGuard(t)
	w := g.do(req{target: "/api/get_config", hdr: map[string]string{"Authorization": "Bearer " + testTok}})
	if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" || w.Header().Get("Access-Control-Allow-Headers") != "" {
		t.Errorf("/api/ reply keeps CORS: %q", v)
	}
	if w.Body.String() != "stub" {
		t.Errorf("body %q", w.Body.String())
	}
	w = g.do(req{target: "/api/nowrite", hdr: map[string]string{"Authorization": "Bearer " + testTok}})
	if v := w.Result().Header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Errorf("/api/ handler that never wrote keeps CORS: %q", v)
	}
	w = g.do(req{target: "/ok"})
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("non-/api/ reply lost its headers (filter must be /api/ only)")
	}
}

func TestFailClosed(t *testing.T) {
	g, _ := testGuard(t)
	g.store = nil
	for target, want := range map[string]int{"/api/get_config": 503, LoginPath: 503, "/": 503, "/ok": 200, "/api/get_ota/x": 200} {
		if w := g.do(req{target: target, hdr: map[string]string{"Authorization": "Bearer " + testTok}}); w.Code != want {
			t.Errorf("fail-closed %s: %d, want %d", target, w.Code, want)
		}
	}
}

func loginForm(pw, next string) string {
	return url.Values{"password": {pw}, "next": {next}}.Encode()
}

func TestLoginSetsStrictCookie(t *testing.T) {
	g, s := testGuard(t)
	w := g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/voice"), hdr: map[string]string{"Origin": "http://localhost:8080"}})
	if w.Code != 303 || w.Header().Get("Location") != "/voice" {
		t.Fatalf("login: %d → %q", w.Code, w.Header().Get("Location"))
	}
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].Name != "mv_session" || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode || c[0].Path != "/" || c[0].Secure {
		t.Fatalf("cookie %+v", c)
	}
	if _, ok := s.Verify(c[0].Value, time.Now()); !ok {
		t.Error("issued cookie does not verify")
	}
	w = g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "//evil"), hdr: map[string]string{"Origin": "http://localhost:8080"}})
	if w.Header().Get("Location") != "/" {
		t.Errorf("login next //evil → %q", w.Header().Get("Location"))
	}
	w = g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, `/./\evil.com`), hdr: map[string]string{"Origin": "http://localhost:8080"}})
	if w.Header().Get("Location") != "/" {
		t.Errorf(`login next /./\evil.com → %q`, w.Header().Get("Location"))
	}
	w = g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: map[string]string{"Origin": "http://evil.example"}})
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Errorf("cross-origin login: %d, cookies %v", w.Code, w.Result().Cookies())
	}
}

func TestLoginRateLimit(t *testing.T) {
	g, _ := testGuard(t)
	now := time.Unix(1_800_000_000, 0)
	g.now = func() time.Time { return now }
	own := map[string]string{"Origin": "http://localhost:8080"}
	for i := 0; i < LoginFailMax; i++ {
		if w := g.do(req{method: "POST", target: LoginPath, body: loginForm("bad", "/"), hdr: own}); w.Code != 401 {
			t.Fatalf("bad login %d: %d", i+1, w.Code)
		}
	}
	w := g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: own})
	if w.Code != 429 || w.Header().Get("Retry-After") != "900" {
		t.Fatalf("6th try: %d Retry-After %q, want 429/900", w.Code, w.Header().Get("Retry-After"))
	}
	if w := g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: own, remote: "10.0.0.2:1"}); w.Code != 303 {
		t.Errorf("other IP blocked: %d", w.Code)
	}
	now = now.Add(LoginWindow)
	if w := g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: own}); w.Code != 303 {
		t.Errorf("after the window: %d", w.Code)
	}
	// Global cap: LoginFailGlobal failures spread over IPs block everyone.
	g2, _ := testGuard(t)
	g2.now = g.now
	for i := 0; i < LoginFailGlobal; i++ {
		g2.do(req{method: "POST", target: LoginPath, body: loginForm("bad", "/"), hdr: own, remote: fmt.Sprintf("10.1.0.%d:1", i)})
	}
	if w := g2.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: own, remote: "10.9.9.9:1"}); w.Code != 429 {
		t.Errorf("global cap: %d, want 429", w.Code)
	}
}

func TestLoginBodyCap(t *testing.T) {
	g, _ := testGuard(t)
	body := loginForm(testPW, "/"+strings.Repeat("a", MaxLoginBody))
	if w := g.do(req{method: "POST", target: LoginPath, body: body, hdr: map[string]string{"Origin": "http://localhost:8080"}}); w.Code != 413 {
		t.Errorf("oversized login: %d, want 413", w.Code)
	}
}

func TestSessionRenew(t *testing.T) {
	g, s := testGuard(t)
	t0 := time.Unix(1_800_000_000, 0)
	v := s.Issue(t0)
	for _, c := range []struct {
		at    time.Duration
		renew bool
	}{{SessionTTL - SessionRenew - time.Hour, false}, {SessionTTL - SessionRenew + time.Hour, true}} {
		now := t0.Add(c.at)
		g.now = func() time.Time { return now }
		w := g.do(req{target: "/", hdr: map[string]string{"Cookie": CookieName + "=" + v}})
		if got := len(w.Result().Cookies()) == 1; w.Code != 200 || got != c.renew {
			t.Errorf("at +%v: %d renewed=%v, want %v", c.at, w.Code, got, c.renew)
		}
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	g, _ := testGuard(t)
	w := g.do(req{target: LogoutPath})
	c := w.Result().Cookies()
	if w.Code != 303 || w.Header().Get("Location") != LoginPath {
		t.Errorf("logout: %d → %q", w.Code, w.Header().Get("Location"))
	}
	if len(c) != 1 || c[0].Name != CookieName || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout Set-Cookie %q", w.Header().Get("Set-Cookie"))
	}
}

func TestLoginParallelLimit(t *testing.T) {
	g, _ := testGuard(t)
	g.sem = make(chan struct{}, 64) // isolate the limiter from load shedding
	own := map[string]string{"Origin": "http://localhost:8080"}
	var wg sync.WaitGroup
	codes := make(chan int, LoginFailMax+20)
	for i := 0; i < LoginFailMax+20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- g.do(req{method: "POST", target: LoginPath, body: loginForm("bad", "/"), hdr: own}).Code
		}()
	}
	wg.Wait()
	close(codes)
	tried := 0
	for c := range codes {
		if c != 429 {
			tried++
		}
	}
	if tried > LoginFailMax {
		t.Errorf("%d parallel guesses got a password check, want <= %d", tried, LoginFailMax)
	}
}

func TestLoginBusy(t *testing.T) {
	g, _ := testGuard(t)
	for i := 0; i < LoginConcurrent; i++ {
		g.sem <- struct{}{}
	}
	w := g.do(req{method: "POST", target: LoginPath, body: loginForm(testPW, "/"), hdr: map[string]string{"Origin": "http://localhost:8080"}})
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Errorf("login with %d checks in flight: %d, want 429", LoginConcurrent, w.Code)
	}
}
