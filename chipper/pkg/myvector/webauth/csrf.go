package webauth

import (
	"net/http"
	"net/url"
)

// csrfOK (D6), cookie-authenticated requests only: no cross-site/same-site fetches, and unsafe methods
// must name this host in Origin (else Referer).
func csrfOK(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	src := r.Header.Get("Origin")
	if src == "" || src == "null" {
		src = r.Referer()
	}
	u, err := url.Parse(src)
	return err == nil && src != "" && u.Host == r.Host
}
