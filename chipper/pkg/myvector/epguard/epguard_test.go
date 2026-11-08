package epguard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Mirrors the real wiring: upstream's catch-all on the same mux as Register.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api-chipper/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "done") })
	mux.HandleFunc("/api-ssh/", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "done") })
	Register(mux)
	return mux
}

func get(mux *http.ServeMux, target string) (int, string) {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

func TestUseIPRefusedWithEmptyBody(t *testing.T) {
	code, body := get(newMux(), UseIPPath+"?port=443")
	if code != http.StatusForbidden || body != "" {
		t.Fatalf("use_ip: got %d %q, want 403 with empty body", code, body)
	}
}

func TestOtherChipperRoutesStillReachUpstream(t *testing.T) {
	for _, p := range []string{"/api-chipper/use_ep", "/api-chipper/restart", UseIPPath + "/", "/api-ssh/get_setup_status"} {
		if code, body := get(newMux(), p); code != http.StatusOK || body != "done" {
			t.Errorf("%s: got %d %q, want upstream's handler", p, code, body)
		}
	}
}

func TestRefuseSSHSetup(t *testing.T) {
	code, body := get(newMux(), SSHSetupPath+"?ip=192.0.2.1")
	if code != http.StatusForbidden || body != "" {
		t.Fatalf("ssh setup: got %d %q, want 403 with empty body", code, body)
	}
}
