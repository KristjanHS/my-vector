package webauth

import "net/http"

// stripCORS drops upstream apiHandler's `Access-Control-Allow-*: *` before the headers go out (D7).
type stripCORS struct {
	http.ResponseWriter
	sent bool
}

func (s *stripCORS) WriteHeader(code int) {
	if !s.sent {
		s.sent = true
		s.Header().Del("Access-Control-Allow-Origin")
		s.Header().Del("Access-Control-Allow-Headers")
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *stripCORS) Write(b []byte) (int, error) {
	if !s.sent {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}
