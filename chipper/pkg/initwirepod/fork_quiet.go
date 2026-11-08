package initwirepod

import (
	"time"

	wirepod_ttr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/ttr"
)

// Fork: re-arm the sound-lock volume restore from the lock file (shut-up-sleep V1). Here, not in ttr, so a test binary never runs it.
func init() {
	time.AfterFunc(wirepod_ttr.ForkQuietStartDelay, wirepod_ttr.ForkQuietRearm)
}
