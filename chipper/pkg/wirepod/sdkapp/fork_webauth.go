package sdkapp

import (
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/webauth"
)

// forkWebHandler: every route on this port behind the shared web-UI auth guard.
func forkWebHandler() http.Handler { return webauth.Handler(http.DefaultServeMux) }
