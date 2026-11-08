// Package epguard keeps wire-pod in Escape Pod mode.
//
// The robot trusts only the Escape Pod certificate (escapepod.local). Upstream's
// /api-chipper/use_ip writes a fresh random certificate for the PC's IP, and the
// robot then shows the crossed-out cloud until it is re-paired over BLE. One save
// in the web UI's setup wizard did that on 2026-11-05. Upstream's /api-ssh/setup
// overwrites /anki/bin/vic-cloud and copies the public ep.crt into the robot's trust (T3).
package epguard

import (
	"net/http"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// UseIPPath is upstream's switch-to-IP-mode endpoint.
const UseIPPath = "/api-chipper/use_ip"

// SSHSetupPath is upstream's robot SSH setup endpoint.
const SSHSetupPath = "/api-ssh/setup"

// Register routes UseIPPath and SSHSetupPath to refusals. An exact path wins over
// upstream's "/api-chipper/" and "/api-ssh/" catch-alls on the same mux, whichever is registered first.
func Register(mux *http.ServeMux) {
	mux.HandleFunc(UseIPPath, Refuse)
	mux.HandleFunc(SSHSetupPath, RefuseSSHSetup)
}

// Refuse answers 403 with an empty body: the setup wizard (webroot/js/initial.js)
// treats any non-empty reply as success, so an empty one is what makes it show
// "Error setting up wire-pod, check the logs" — and the log says why.
func Refuse(w http.ResponseWriter, r *http.Request) {
	logger.Println("my-vector: refused " + UseIPPath + " from " + r.RemoteAddr + " — the robot trusts only the Escape Pod certificate; pick Escape Pod (/api-chipper/use_ep)")
	w.WriteHeader(http.StatusForbidden)
}

// RefuseSSHSetup answers 403 with an empty body, like Refuse.
func RefuseSSHSetup(w http.ResponseWriter, r *http.Request) {
	logger.Println("my-vector: refused " + SSHSetupPath + " from " + r.RemoteAddr + " — it replaces vic-cloud and trusts the public ep.crt; robot setup is robot/firmware/ship.sh")
	w.WriteHeader(http.StatusForbidden)
}
