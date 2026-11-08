package sdkapp

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

// soundPaths is G5's list, typed independently of forkQuietPaths so dropping one there fails here.
var soundPaths = []string{"say_text", "play_sound", "trigger_wake_word", "button_hey_vector", "button_alexa", "cloud_intent"}

var (
	catchAllOnce sync.Once
	catchAllHit  http.HandlerFunc
)

// muxed serves path through http.DefaultServeMux (fork inits + a stub "/api-sdk/" catch-all) and reports which stub ran.
func muxed(t *testing.T, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var hit string
	catchAllOnce.Do(func() {
		http.HandleFunc("/api-sdk/", func(w http.ResponseWriter, r *http.Request) { catchAllHit(w, r) })
	})
	catchAllHit = func(w http.ResponseWriter, r *http.Request) { hit = "catch-all" }
	old := forkQuietNext
	forkQuietNext = func(w http.ResponseWriter, r *http.Request) { hit = "next" }
	t.Cleanup(func() { forkQuietNext = old })
	w := httptest.NewRecorder()
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	http.DefaultServeMux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+sep+"text=x", nil))
	return w, hit
}

func lock(t *testing.T, on bool) {
	t.Helper()
	t.Setenv("MYVECTOR_QUIET_FILE", filepath.Join(t.TempDir(), "quiet-until"))
	if on {
		if err := quiet.Set(time.Now().Add(time.Hour), "test"); err != nil {
			t.Fatal(err)
		}
	}
}

func wantQuiet(t *testing.T, name string, w *httptest.ResponseRecorder) {
	t.Helper()
	body := w.Body.String()
	if w.Code != http.StatusLocked || !strings.HasPrefix(body, "QUIET: robot sound locked until ") || !strings.Contains(body, "(test); make unquiet lifts it") {
		t.Errorf("%s locked: %d %q, want 423 + QUIET line", name, w.Code, body)
	}
}

func TestForkQuietBlocksSound(t *testing.T) {
	for _, p := range soundPaths {
		lock(t, true)
		w, hit := muxed(t, "/api-sdk/"+p)
		wantQuiet(t, p, w)
		if hit != "" {
			t.Errorf("%s locked: reached %s", p, hit)
		}
		lock(t, false)
		if w, hit = muxed(t, "/api-sdk/"+p); hit != "next" || w.Code != http.StatusOK {
			t.Errorf("%s unlocked: %d, reached %q, want the SdkapiHandler stub", p, w.Code, hit)
		}
	}
}

func TestForkQuietLeavesSilent(t *testing.T) {
	lock(t, true)
	for _, p := range []string{"get_battery", "volume"} {
		if w, hit := muxed(t, "/api-sdk/"+p); hit != "catch-all" || w.Code != http.StatusOK {
			t.Errorf("%s locked: %d, reached %q, want the catch-all", p, w.Code, hit)
		}
	}
}

// TestForkQuietLogIntentPasses: smoke.sh A3's log-level intents run while locked (UD1); any other cloud_intent stays refused.
func TestForkQuietLogIntentPasses(t *testing.T) {
	lock(t, true)
	if w, hit := muxed(t, "/api-sdk/cloud_intent?intent=my_vector_log_debug"); hit != "next" || w.Code != http.StatusOK {
		t.Errorf("log intent locked: %d, reached %q, want the SdkapiHandler stub", w.Code, hit)
	}
	w, _ := muxed(t, "/api-sdk/cloud_intent?intent=intent_greeting_hello")
	wantQuiet(t, "cloud_intent greeting", w)
}

func TestForkSayQuiet(t *testing.T) {
	lock(t, true)
	w := httptest.NewRecorder()
	forkSay(w, httptest.NewRequest(http.MethodGet, "/api-sdk/fork_say?text=x", nil))
	wantQuiet(t, "fork_say", w)
}
