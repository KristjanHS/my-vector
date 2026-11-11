package sdkapp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/webauth"
)

func TestForkWebHandlerGuards(t *testing.T) {
	d := t.TempDir()
	webauth.DefaultPaths = webauth.Paths{Auth: filepath.Join(d, "webauth.json"), Password: filepath.Join(d, "pw"), Header: filepath.Join(d, "hdr")}
	h := forkWebHandler()
	for target, want := range map[string]int{"/api-sdk/x": http.StatusUnauthorized, "/cam-stream": http.StatusUnauthorized} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != want {
			t.Errorf("%s without creds: %d, want %d", target, w.Code, want)
		}
	}
}
