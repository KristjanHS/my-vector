package initwirepod

import (
	"context"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/wifi"
)

// Fork: roam the robot to a stronger saved WiFi network (myvector/wifi). Here, not in sdkapp, so only the server
// binaries run it — never a test binary, which would SSH into the live robot.
func init() {
	go wifi.Roam(context.Background())
}
