package sdkapp

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestForkCaptureUnknownSerial(t *testing.T) {
	w := httptest.NewRecorder()
	forkCapture(w, httptest.NewRequest("GET", "/api-sdk/fork_capture?serial=00000000", nil))
	if body := w.Body.String(); !strings.HasPrefix(body, "error: ") {
		t.Fatalf("body %q, want error: …", body)
	}
	if ct := w.Header().Get("Content-Type"); ct == "image/jpeg" {
		t.Fatalf("Content-Type %q on an error", ct)
	}
}
