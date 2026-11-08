package sdkapp

import (
	"net/http"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

// forkQuietPaths make robot sound; each exact pattern beats the upstream "/api-sdk/" catch-all.
// Registering one of these exactly anywhere else panics at init.
var forkQuietPaths = []string{"say_text", "play_sound", "trigger_wake_word", "button_hey_vector", "button_alexa", "cloud_intent"}

// forkQuietNext is the fall-through when unlocked; tests stub it.
var forkQuietNext http.HandlerFunc = SdkapiHandler

func init() {
	for _, p := range forkQuietPaths {
		http.HandleFunc("/api-sdk/"+p, forkQuietGate)
	}
}

// forkQuietSilentIntent is the cloud_intent prefix that makes no sound (patch 0007 log level; smoke.sh A3 runs it while locked, UD1).
const forkQuietSilentIntent = "my_vector_log_"

// forkQuietGate refuses with 423 + the QUIET line while the lock holds, else runs the upstream handler.
func forkQuietGate(w http.ResponseWriter, r *http.Request) {
	silent := r.URL.Path == "/api-sdk/cloud_intent" && strings.HasPrefix(r.FormValue("intent"), forkQuietSilentIntent)
	if !silent && quiet.Refuse(w) {
		return
	}
	forkQuietNext(w, r)
}
