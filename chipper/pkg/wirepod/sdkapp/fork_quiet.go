package sdkapp

import (
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/scripting"
)

// forkQuietPaths make robot sound; each exact pattern beats the upstream "/api-sdk/" catch-all.
// Registering one of these exactly anywhere else panics at init.
var forkQuietPaths = []string{"say_text", "play_sound", "trigger_wake_word", "button_hey_vector", "button_alexa", "cloud_intent"}

// forkQuietNext is the fall-through when unlocked; tests stub it.
var forkQuietNext http.HandlerFunc = SdkapiHandler

// forkQuietLuaPath runs SayText/PlayAnimation scripts; the exact pattern beats upstream's "/api-lua/" catch-all.
const forkQuietLuaPath = "/api-lua/run_script"

// forkQuietLuaNext is the unlocked fall-through for forkQuietLuaPath; tests stub it.
var forkQuietLuaNext http.HandlerFunc = scripting.ScriptingAPI

func init() {
	for _, p := range forkQuietPaths {
		http.HandleFunc("/api-sdk/"+p, forkQuietGate)
	}
	http.HandleFunc(forkQuietLuaPath, func(w http.ResponseWriter, r *http.Request) {
		if !quiet.Refuse(w) {
			forkQuietLuaNext(w, r)
		}
	})
}

// forkQuietSilentIntents are the only cloud_intents passed while locked: patch 0007's log levels make no sound (smoke.sh A3, UD1).
var forkQuietSilentIntents = map[string]bool{"my_vector_log_debug": true, "my_vector_log_info": true}

// forkQuietGate refuses with 423 + the QUIET line while the lock holds, else runs the upstream handler.
func forkQuietGate(w http.ResponseWriter, r *http.Request) {
	silent := r.URL.Path == "/api-sdk/cloud_intent" && forkQuietSilentIntents[r.FormValue("intent")]
	if !silent && quiet.Refuse(w) {
		return
	}
	forkQuietNext(w, r)
}
