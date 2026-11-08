package initwirepod

import wirepod_ttr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/ttr"

// Fork: the robot mic stream receiver on :8093 (myvector/listen Stream). Here, not in ttr, so a test binary never binds it.
func init() {
	go wirepod_ttr.ForkServeStream()
}
