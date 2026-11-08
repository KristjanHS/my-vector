package wirepod_ttr

import (
	"errors"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// forkMicStream receives the robot mic stream (M5); Hear runs in arrival order, the LLM reply apart.
var forkMicStream = &listen.Stream{
	ESN:  forkStreamESN,
	Lang: lang.Current,
	Log:  func(l string) { logger.Println(l) },
}

// forkArm points a listening robot's mic stream at :8093 over its :8889 console (M6).
var forkArm = &listen.Arm{
	Set:    forkSetStreamPort,
	Active: func(esn string) bool { return forkListening.Active(esn, time.Now()) },
	Log:    func(l string) { logger.Println(l) },
}

func init() {
	forkMicStream.Heard = forkStreamHeard
	forkMicStream.Up, forkMicStream.Down, forkArm.Close = forkArm.Up, forkArm.Down, forkMicStream.Close
}

func forkStreamHeard(u listen.Utterance) {
	if reply := forkHearer.Hear(u); reply != nil {
		go reply()
	}
}

func forkSetStreamPort(esn string, port int) error {
	ip := forkRobotIP(esn)
	if ip == "" {
		return errors.New("no IP for " + esn)
	}
	return listen.SetPort(ip, port)
}

// forkStreamESN is the robot whose last jdocs/token connection came from ip (botSdkInfo.json), else "".
func forkStreamESN(ip string) string {
	for _, r := range vars.BotInfo.Robots {
		if r.IPAddress == ip {
			return r.Esn
		}
	}
	return ""
}

// ForkServeStream serves listen.StreamAddr; initwirepod starts it, so only the server binaries listen — never a test binary.
func ForkServeStream() {
	if err := forkMicStream.ListenAndServe(); err != nil {
		logger.Println("myvector listen: stream: " + err.Error())
	}
}
