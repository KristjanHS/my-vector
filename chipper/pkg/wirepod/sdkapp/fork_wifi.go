package sdkapp

import (
	"context"
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/wifi"
)

// Fork WiFi page: the robot's saved networks (myvector/wifi), on the default mux the web UI serves, and Roam.
func init() {
	http.HandleFunc(wifi.Page, wifi.PageHandler)
	http.HandleFunc(wifi.API, wifi.Handler)
	go wifi.Roam(context.Background())
}
