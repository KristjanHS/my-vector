package wirepod_ttr

import (
	"strings"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

// forkSayText speaks an LLM answer in the current language's saved T1 voice (DoSayText's hook, plan B3); an interrupt cuts it.
func forkSayText(input string, robot *vector.Vector) error {
	from := time.Now()
	used, err := voice.Speak(forkSpeakCtx(robot), robot, input, voice.Current().For(lang.Current()))
	forkListening.Spoken(robot.Cfg.SerialNo, from, time.Now())
	if err != nil {
		logger.Println("myvector voice " + used + ": " + err.Error())
	} else if strings.HasPrefix(used, voice.Fallback) {
		logger.Println("myvector voice " + used)
	}
	return err
}
