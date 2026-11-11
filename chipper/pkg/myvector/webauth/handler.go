package webauth

import (
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	LoginPath  = "/myvector/login"
	LogoutPath = "/myvector/logout"
)

// DefaultPaths is what Handler loads; set before the first Handler call.
var DefaultPaths Paths

var (
	once   sync.Once
	shared http.Handler
)

// Handler is the process-wide guard (one store, one rate limiter) around next; both web ports share it.
func Handler(next http.Handler) http.Handler {
	once.Do(func() { shared = New(DefaultPaths, next) })
	return shared
}

// New loads p (bootstrapping it when absent) and guards next. A store that fails to load fails closed.
func New(p Paths, next http.Handler) http.Handler {
	s, err := Load(p)
	if err != nil {
		logln("webauth: FAIL CLOSED — every web path but the robot's answers 503 until fixed or deleted:", err)
	}
	return newGuard(s, next)
}

type guard struct {
	store *Store // nil = fail closed
	next  http.Handler
	lim   *limiter
	now   func() time.Time
}

func newGuard(s *Store, next http.Handler) *guard {
	return &guard{store: s, next: next, lim: newLimiter(), now: time.Now}
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case exempt(r):
		g.next.ServeHTTP(w, r)
		return
	case g.store == nil:
		http.Error(w, "web auth store unusable — see the wire-pod log", http.StatusServiceUnavailable)
		return
	case r.URL.Path == LoginPath:
		g.login(w, r)
		return
	case r.URL.Path == LogoutPath:
		g.logout(w, r)
		return
	}
	if h := r.Header.Get("Authorization"); h != "" {
		if !strings.HasPrefix(h, "Bearer ") || !g.store.CheckToken(strings.TrimPrefix(h, "Bearer ")) {
			g.unauthorized(w, r)
			return
		}
		g.serve(w, r)
		return
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		g.unauthorized(w, r)
		return
	}
	now := g.now()
	exp, ok := g.store.Verify(c.Value, now)
	if !ok {
		g.unauthorized(w, r)
		return
	}
	if !csrfOK(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	if exp.Sub(now) < SessionRenew {
		g.setSession(w, now)
	}
	g.serve(w, r)
}

func (g *guard) serve(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w = &stripCORS{ResponseWriter: w}
	}
	g.next.ServeHTTP(w, r)
}

// exempt: the robot's own unauthenticated calls (D5). Login/logout are routed by ServeHTTP.
func exempt(r *http.Request) bool {
	p := r.URL.Path
	if path.Clean(p) != p {
		return false
	}
	return p == "/ok" || p == "/ok:80" || (r.Method == http.MethodGet && strings.HasPrefix(p, "/api/get_ota/"))
}

func (g *guard) unauthorized(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, LoginPath+"?next="+url.QueryEscape(safeNext(r.URL.RequestURI())), http.StatusSeeOther)
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="wire-pod"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// safeNext keeps a same-site local path, anything else (//host, /\host, absolute URLs) becomes "/".
func safeNext(n string) string {
	if !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, `/\`) {
		return "/"
	}
	return n
}
