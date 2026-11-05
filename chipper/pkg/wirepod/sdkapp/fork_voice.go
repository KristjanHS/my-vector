package sdkapp

import (
	"context"
	"errors"
	"net/http"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// Fork voice settings (plan B2) and demo (R14), on the default mux the web UI serves.
func init() {
	http.HandleFunc(voice.SettingsPage, voice.PageHandler)
	http.HandleFunc(voice.SettingsAPI, voice.SettingsHandler)
	http.HandleFunc(voice.DemoAPI, voice.DemoHandler(forkVoiceRobot))
}

// forkVoiceRobot is the ?serial= robot, or the only paired one.
func forkVoiceRobot(r *http.Request) (context.Context, *vector.Vector, error) {
	serial := r.URL.Query().Get("serial")
	if serial == "" {
		if len(vars.BotInfo.Robots) != 1 {
			return nil, nil, errors.New("not exactly one paired robot: add ?serial=<esn>")
		}
		serial = vars.BotInfo.Robots[0].Esn
	}
	robot, index, err := getRobot(serial)
	if err != nil {
		return nil, nil, err
	}
	robots[index].ConnTimer = 0
	return robot.Ctx, robot.Vector, nil
}
