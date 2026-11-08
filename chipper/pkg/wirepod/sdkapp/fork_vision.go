package sdkapp

// fork_capture serves one high-res camera frame as raw JPEG for robot/vision-bench/grab.sh.
// Camera read only: no SayText, animation, mirror mode or motion.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
)

func init() {
	http.HandleFunc("/api-sdk/fork_capture", forkCapture)
}

// forkCapture writes the JPEG with Content-Type image/jpeg, or "error: …".
func forkCapture(w http.ResponseWriter, r *http.Request) {
	robot, ok := forkRobot(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(robot.Ctx, 15*time.Second)
	defer cancel()
	resp, err := robot.Vector.Conn.CaptureSingleImage(ctx, &vectorpb.CaptureSingleImageRequest{EnableHighResolution: true})
	if err != nil {
		fmt.Fprint(w, "error: "+err.Error())
		return
	}
	if len(resp.GetData()) == 0 {
		fmt.Fprint(w, "error: empty image")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(resp.GetData())
}
