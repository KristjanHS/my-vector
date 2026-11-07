package wirepod_ttr

import (
	"errors"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// forkQuiet runs ahead of every other route (ProcessTextAll): a whole-phrase "shut up" locks robot sound, naps the robot
// and sends intent_system_sleep. Anything else returns false and routes on.
func forkQuiet(req interface{}, voiceText, botSerial string) bool {
	phrase, ok := quiet.Match(voiceText)
	if !ok {
		return false
	}
	now := time.Now()
	if err := quiet.Set(now.Add(quiet.LockFor), "voice:"+phrase); err != nil {
		logger.Println("my-vector quiet: lock: " + err.Error())
	}
	go forkNap(botSerial, now.Add(quiet.NapFor))
	IntentPass(req, "intent_system_sleep", voiceText, map[string]string{}, false)
	return true
}

func forkNap(esn string, until time.Time) {
	ip := ""
	for _, r := range vars.BotInfo.Robots {
		if r.Esn == esn {
			ip = r.IPAddress
		}
	}
	if ip == "" {
		logger.Println("my-vector quiet: nap skipped: no IP for " + esn)
		return
	}
	if err := quiet.Nap(ip, until); errors.Is(err, quiet.ErrNapOff) {
		logger.Println("my-vector quiet: nap skipped: " + err.Error())
	} else if err != nil {
		logger.Println("my-vector quiet: nap: " + err.Error())
	} else {
		logger.Println("my-vector quiet: nap until " + until.Format(time.RFC3339) + " on " + esn)
	}
}
