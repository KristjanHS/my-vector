package wirepod_ttr

import (
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// forkMicStream receives the robot mic stream (M5, Stage 2.1: utterances are logged only; Heard + arming are Stage 2.2).
var forkMicStream = &listen.Stream{
	ESN:  forkStreamESN,
	Lang: lang.Current,
	Log:  func(l string) { logger.Println(l) },
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
