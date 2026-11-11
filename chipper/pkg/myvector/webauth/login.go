package webauth

import (
	_ "embed"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"time"
)

//go:embed login.html
var loginHTML string

var loginPage = template.Must(template.New("login").Parse(loginHTML))

func (g *guard) login(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		g.page(w, http.StatusOK, safeNext(r.URL.Query().Get("next")), "")
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r) {
		http.Error(w, "cross-site login refused", http.StatusForbidden)
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := g.now()
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	default: // bcrypt checks already in flight: shed load, don't queue CPU
		w.Header().Set("Retry-After", "1")
		http.Error(w, "login busy — try again", http.StatusTooManyRequests)
		return
	}
	if d := g.lim.try(ip, now); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds()+0.999)))
		http.Error(w, "too many failed logins — try again later", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxLoginBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad login form", http.StatusRequestEntityTooLarge)
		return
	}
	next := safeNext(r.PostForm.Get("next"))
	if !g.store.CheckPassword(r.PostForm.Get("password")) {
		g.page(w, http.StatusUnauthorized, next, "Wrong password.")
		return
	}
	g.lim.forgive(ip, now)
	g.setSession(w, now)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (g *guard) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, LoginPath, http.StatusSeeOther)
}

func (g *guard) setSession(w http.ResponseWriter, now time.Time) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: g.store.Issue(now), Path: "/",
		MaxAge: int(SessionTTL / time.Second), HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (g *guard) page(w http.ResponseWriter, code int, next, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(code)
	loginPage.Execute(w, struct{ Action, Next, Msg string }{LoginPath, next, msg})
}
