package sdkapp

// Fork endpoints for robot/firmware/smoke.sh. Exact patterns win over the
// upstream "/api-sdk/" catch-all, so no upstream file changes.
// Never commands the wheels or lift: check-static.sh S5 greps this file.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
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
			"wheels_moving":  s.GetStatus()&uint32(vectorpb.RobotStatus_ROBOT_STATUS_ARE_WHEELS_MOVING) != 0,
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

// forkSay speaks ?text= and writes the final utterance state (FINISHED on success).
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
	ctx, cancel := context.WithTimeout(robot.Ctx, 20*time.Second)
	defer cancel()
	resp, err := robot.Vector.Conn.SayText(ctx, &vectorpb.SayTextRequest{
		DurationScalar: 1,
		UseVectorVoice: true,
		Text:           text,
	})
	if err != nil {
		fmt.Fprint(w, "error: "+err.Error())
		return
	}
	fmt.Fprint(w, resp.GetState().String())
}
