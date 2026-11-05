package initwirepod

import (
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/epguard"
)

// Fork: refuse the switch to IP mode (see epguard). Same mux as ChipperHTTPApi.
func init() {
	epguard.Register(http.DefaultServeMux)
}
