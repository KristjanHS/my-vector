package initwirepod

import (
	"context"
	"os"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/usagepoll"
)

// Fork: the hourly robot poll for the usage ledger (myvector/usagepoll). Here, not in a shared package, so only the
// server binaries run it — never a test binary, which would SSH into the live robot (the .test check guards a future
// initwirepod test).
func init() {
	if strings.HasSuffix(os.Args[0], ".test") {
		return
	}
	go usagepoll.Default().Loop(context.Background(), usagepoll.First, usagepoll.Every)
}
