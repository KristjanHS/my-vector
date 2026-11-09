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

// soundPaths is G5's list → the unlocked fall-through stub, typed independently of the fork lists so dropping one there fails here.
var soundPaths = map[string]string{
	"/api-sdk/say_text": "next", "/api-sdk/play_sound": "next", "/api-sdk/trigger_wake_word": "next",
	"/api-sdk/button_hey_vector": "next", "/api-sdk/button_alexa": "next", "/api-sdk/cloud_intent": "next",
	"/api-lua/run_script": "lua",
}

var (
	catchAllOnce sync.Once
	catchAllHit  http.HandlerFunc
)

// muxed serves path through http.DefaultServeMux (fork inits + stub "/api-sdk/" and "/api-lua/" catch-alls) and reports which stub ran.
func muxed(t *testing.T, path string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var hit string
	catchAllOnce.Do(func() {
		http.HandleFunc("/api-sdk/", func(w http.ResponseWriter, r *http.Request) { catchAllHit(w, r) })
		http.HandleFunc("/api-lua/", func(w http.ResponseWriter, r *http.Request) { catchAllHit(w, r) })
	})
	catchAllHit = func(w http.ResponseWriter, r *http.Request) { hit = "catch-all" }
	old, oldLua := forkQuietNext, forkQuietLuaNext
	forkQuietNext = func(w http.ResponseWriter, r *http.Request) { hit = "next" }
	forkQuietLuaNext = func(w http.ResponseWriter, r *http.Request) { hit = "lua" }
	t.Cleanup(func() { forkQuietNext, forkQuietLuaNext = old, oldLua })
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
	for p, next := range soundPaths {
		lock(t, true)
		w, hit := muxed(t, p)
		wantQuiet(t, p, w)
		if hit != "" {
			t.Errorf("%s locked: reached %s", p, hit)
		}
		lock(t, false)
		if w, hit = muxed(t, p); hit != next || w.Code != http.StatusOK {
			t.Errorf("%s unlocked: %d, reached %q, want the %s stub", p, w.Code, hit, next)
		}
	}
}

func TestForkQuietLeavesSilent(t *testing.T) {
	lock(t, true)
	for _, p := range []string{"/api-sdk/get_battery", "/api-sdk/volume", "/api-lua/other"} {
		if w, hit := muxed(t, p); hit != "catch-all" || w.Code != http.StatusOK {
			t.Errorf("%s locked: %d, reached %q, want the catch-all", p, w.Code, hit)
		}
	}
}

// TestForkQuietLogIntentPasses: patch 0007's two log-level intents run while locked (UD1); any other cloud_intent stays refused.
func TestForkQuietLogIntentPasses(t *testing.T) {
	lock(t, true)
	for _, i := range []string{"my_vector_log_debug", "my_vector_log_info"} {
		if w, hit := muxed(t, "/api-sdk/cloud_intent?intent="+i); hit != "next" || w.Code != http.StatusOK {
			t.Errorf("%s locked: %d, reached %q, want the SdkapiHandler stub", i, w.Code, hit)
		}
	}
	for _, i := range []string{"intent_greeting_hello", "my_vector_log_x"} {
		w, hit := muxed(t, "/api-sdk/cloud_intent?intent="+i)
		wantQuiet(t, i, w)
		if hit != "" {
			t.Errorf("%s locked: reached %s", i, hit)
		}
	}
}

func TestForkSayQuiet(t *testing.T) {
	lock(t, true)
	w := httptest.NewRecorder()
	forkSay(w, httptest.NewRequest(http.MethodGet, "/api-sdk/fork_say?text=x", nil))
	wantQuiet(t, "fork_say", w)
}
