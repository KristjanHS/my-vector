package webauth

import "net/http"

// stripCORS drops upstream apiHandler's `Access-Control-Allow-*: *` before the headers go out (D7).
type stripCORS struct {
	http.ResponseWriter
	sent bool
}

func (s *stripCORS) strip() {
	if !s.sent {
		s.sent = true
		s.Header().Del("Access-Control-Allow-Origin")
		s.Header().Del("Access-Control-Allow-Headers")
	}
}

func (s *stripCORS) WriteHeader(code int) {
	s.strip()
	s.ResponseWriter.WriteHeader(code)
}

func (s *stripCORS) Write(b []byte) (int, error) {
	s.strip()
	return s.ResponseWriter.Write(b)
}

func (s *stripCORS) Flush() {
	s.strip()
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *stripCORS) Unwrap() http.ResponseWriter { return s.ResponseWriter }
