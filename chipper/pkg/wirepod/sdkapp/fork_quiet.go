package sdkapp

import (
	"net/http"

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

// forkQuietGate refuses with 423 + the QUIET line while the lock holds, else runs the upstream handler.
func forkQuietGate(w http.ResponseWriter, r *http.Request) {
	if quiet.Refuse(w) {
		return
	}
	forkQuietNext(w, r)
}
