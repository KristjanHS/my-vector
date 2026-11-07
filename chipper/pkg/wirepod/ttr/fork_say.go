package wirepod_ttr

import (
	"context"
	"regexp"
	"strings"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

// forkSayText speaks an LLM answer in the current language's saved T1 voice (DoSayText's hook, plan B3).
func forkSayText(input string, robot *vector.Vector) error {
	input = forkClockColon(input)
	used, err := voice.Speak(context.Background(), robot, input, voice.Current().For(lang.Current()))
	if err != nil {
		logger.Println("myvector voice " + used + ": " + err.Error())
	} else if strings.HasPrefix(used, voice.Fallback) {
		logger.Println("myvector voice " + used)
	}
	return err
}

// forkKellDot is an Estonian "kell [on] H.MM" / "kella H.MM" time: the dot separator the language's style guide uses.
var forkKellDot = regexp.MustCompile(`(?i)\b(kella?(?:\s+on)?\s+(?:[01]?\d|2[0-3]))\.([0-5]\d)\b`)

// forkClockColon writes "kell 10.10" as "kell 10:10": the user's format for hour and minute.
// Only after "kell"/"kella", so a decimal like 3.14 stays as it is.
func forkClockColon(s string) string {
	return forkKellDot.ReplaceAllString(s, "$1:$2")
}
