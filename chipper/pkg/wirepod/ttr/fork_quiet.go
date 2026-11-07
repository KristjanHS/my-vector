package wirepod_ttr

import (
	"errors"
	"sync"
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
	forkQuietLock(botSerial, phrase, time.Now())
	IntentPass(req, "intent_system_sleep", voiceText, map[string]string{}, false)
	return true
}

// forkQuietLock is a "shut up" match's lock + listen off + nap (wake-word and overheard alike).
func forkQuietLock(esn, phrase string, now time.Time) {
	if err := quiet.Set(now.Add(quiet.LockFor), "voice:"+phrase); err != nil {
		logger.Println("my-vector quiet: lock: " + err.Error())
	}
	forkListening.Deactivate(esn, "quiet")
	ip := forkRobotIP(esn)
	forkNaps.Add(1)
	go func() { defer forkNaps.Done(); forkNap(ip, esn, now.Add(quiet.NapFor)) }()
}

// forkNaps tracks in-flight naps so tests can wait them out.
var forkNaps sync.WaitGroup

func forkRobotIP(esn string) string {
	for _, r := range vars.BotInfo.Robots {
		if r.Esn == esn {
			return r.IPAddress
		}
	}
	return ""
}

func forkNap(ip, esn string, until time.Time) {
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
