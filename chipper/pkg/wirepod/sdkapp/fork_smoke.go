package sdkapp

// Fork endpoints for robot/firmware/smoke.sh. Exact patterns win over the
// upstream "/api-sdk/" catch-all, so no upstream file changes.
// Never commands the wheels or lift: check-static.sh S5 fails on a wheel/lift
// name here or in myvector/voice (denylist) and on any Conn method but EventStream,
// DisplayFaceImageRGB and SayText here, ExternalAudioStreamPlayback/SayText there (allowlist).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

const (
	faceW, faceH = 184, 96
	faceGreen    = 0x07E0 // RGB565
)

func init() {
	http.HandleFunc("/api-sdk/fork_robot_state", forkRobotState)
	http.HandleFunc("/api-sdk/fork_face", forkFace)
	http.HandleFunc("/api-sdk/fork_say", forkSay)
}

// forkRobot looks up the robot by ?serial=, or writes "error: …" and returns false.
func forkRobot(w http.ResponseWriter, r *http.Request) (Robot, bool) {
	robot, index, err := getRobot(r.FormValue("serial"))
	if err != nil {
		fmt.Fprint(w, "error: "+err.Error())
		return robot, false
	}
	robots[index].ConnTimer = 0
	return robot, true
}

// forkRobotState writes the first robot_state event as JSON.
func forkRobotState(w http.ResponseWriter, r *http.Request) {
	robot, ok := forkRobot(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(robot.Ctx, 10*time.Second)
	defer cancel()
	stream, err := robot.Vector.Conn.EventStream(ctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{
			WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}},
		},
		ConnectionId: "wirepod-fork-smoke",
	})
	if err != nil {
		fmt.Fprint(w, "error: "+err.Error())
		return
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			fmt.Fprint(w, "error: "+err.Error())
			return
		}
		s := resp.GetEvent().GetRobotState()
		if s == nil {
			continue
		}
		p := s.GetPose()
		if p == nil { // a nil pose reads as 0,0,0 and would make smoke.sh A4 unfailable
			fmt.Fprint(w, "error: robot_state has no pose")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"head_angle_rad": s.GetHeadAngleRad(),
			"pose":           map[string]float32{"x": p.GetX(), "y": p.GetY(), "z": p.GetZ(), "q0": p.GetQ0()},
			"origin_id":      p.GetOriginId(),
			"status":         s.GetStatus(),
			// smoke.sh A4 bounds the measured speeds; the ARE_WHEELS_MOVING flag (any speed > 1e-5 mm/s, or point-turn mode) is shown, not checked
			"wheels_mm_s":   map[string]float32{"l": s.GetLeftWheelSpeedMmps(), "r": s.GetRightWheelSpeedMmps()},
			"wheels_moving": s.GetStatus()&uint32(vectorpb.RobotStatus_ROBOT_STATUS_ARE_WHEELS_MOVING) != 0,
		})
		return
	}
}

// forkFace shows a solid green face for 2 s; writes "ok" or the error.
func forkFace(w http.ResponseWriter, r *http.Request) {
	robot, ok := forkRobot(w, r)
	if !ok {
		return
	}
	face := make([]byte, faceW*faceH*2)
	for i := 0; i < len(face); i += 2 {
		face[i], face[i+1] = faceGreen>>8, faceGreen&0xFF
	}
	ctx, cancel := context.WithTimeout(robot.Ctx, 10*time.Second)
	defer cancel()
	_, err := robot.Vector.Conn.DisplayFaceImageRGB(ctx, &vectorpb.DisplayFaceImageRGBRequest{
		FaceData:         face,
		DurationMs:       2000,
		InterruptRunning: true,
	})
	if err != nil {
		fmt.Fprint(w, "error: "+err.Error())
		return
	}
	fmt.Fprint(w, "ok")
}

// forkSay speaks ?text= in the saved language's T1 (waiting up to 7 s for PC audio) and writes "FINISHED <voice>", "FINISHED fallback: <why>" or the error.
func forkSay(w http.ResponseWriter, r *http.Request) {
	robot, ok := forkRobot(w, r)
	if !ok {
		return
	}
	text := r.FormValue("text")
	if text == "" || len([]rune(text)) >= 600 {
		fmt.Fprint(w, "error: text must be 1-599 characters")
		return
	}
	// 7 s first-audio wait + smoke.sh's longest phrase (~10 s at slow 1.2) + a Stall's slack; under its fork_say curl -m 35
	ctx, cancel := context.WithTimeout(robot.Ctx, 30*time.Second)
	defer cancel()
	// SayText (stock T1 and the fallback) is refused without behavior control, as in voice.DemoHandler
	release, err := voice.Control(ctx, robot.Vector)
	if err != nil {
		fmt.Fprint(w, "error: robot control: "+err.Error())
		return
	}
	defer release()
	// The saved language's T1 at the saved speed; 7 s outlasts a cold Kokoro load (6.6 s seen) before falling back.
	used, err := voice.Speak(voice.WithFirstAudio(ctx, 7*time.Second), robot.Vector, text, voice.Current().For(lang.Current()))
	if err != nil {
		fmt.Fprint(w, "error: "+used+": "+err.Error())
		return
	}
	fmt.Fprint(w, "FINISHED "+used)
}
