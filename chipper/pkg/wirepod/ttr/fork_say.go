package wirepod_ttr

import (
	"context"
	"strings"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

// forkSayText speaks an LLM answer in the saved T1 voice (DoSayText's hook, plan B3).
func forkSayText(input string, robot *vector.Vector) error {
	used, err := voice.Speak(context.Background(), robot, input, voice.Current())
	if err != nil {
		logger.Println("myvector voice " + used + ": " + err.Error())
	} else if strings.HasPrefix(used, voice.Fallback) {
		logger.Println("myvector voice " + used)
	}
	return err
}
