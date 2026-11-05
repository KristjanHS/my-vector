package sdkapp

import (
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

// Fork voice settings (plan B2), on the default mux the web UI serves.
func init() {
	http.HandleFunc(voice.SettingsPage, voice.PageHandler)
	http.HandleFunc(voice.SettingsAPI, voice.SettingsHandler)
}
