package wirepod_ttr

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func init() {
	memory.Pass = forkMemoryPass
	forkStartMemory(os.Args[0])
}

// forkStartMemory starts the nightly loop (M21) unless bin is a go test binary; true = started.
func forkStartMemory(bin string) bool {
	if strings.HasSuffix(bin, ".test") {
		return false
	}
	go memory.Loop(forkMemoryZone)
	return true
}

// forkMemoryZone is the first paired robot's zone, else time.Local (M20).
func forkMemoryZone() *time.Location {
	if r := vars.BotInfo.Robots; len(r) > 0 {
		return forkBotZone(r[0].Esn)
	}
	return time.Local
}

// forkMemoryPass is one memory.Run on the memory target with each robot's zone; off or no target → an error, nothing run.
func forkMemoryPass(ctx context.Context) error {
	t, ok := memory.Target(llm.Load(), lang.Current(), forkKG())
	if !ok {
		return errors.New("memory model " + memory.Model() + ": off or unavailable (see the log)")
	}
	return memory.Run(ctx, t, forkBotZone)
}

// forkMemory is esn's {facts} and {diary} for utterance on brain; a var so tests fake it.
var forkMemory = func(esn, utterance, brain string) (facts, diary string) {
	s, err := memory.Read()
	if err != nil {
		logger.Println("myvector: memory read:", err)
		return "", ""
	}
	return memory.Block(s, esn, utterance, brain)
}

// forkCaptureChat (top of Remember, M3) appends an LLM turn; nothing without Knowledge.SaveChat.
func forkCaptureChat(user, ai, esn string) {
	memory.Touch()
	if !vars.APIConfig.Knowledge.SaveChat {
		return
	}
	forkEpisode(esn, memory.Episode{Kind: memory.Chat, User: user, Reply: ai})
}

// forkCaptureIntent (IntentPass, M4) appends an intent turn; skips intent_system_* and an LLM-marked intent_greeting_hello.
func forkCaptureIntent(esn, intentThing, speechText string) {
	memory.Touch()
	if strings.HasPrefix(intentThing, "intent_system_") || strings.TrimSpace(speechText) == "" {
		return
	}
	if intentThing == "intent_greeting_hello" && memory.TakeLLM(esn, speechText) {
		return
	}
	forkEpisode(esn, memory.Episode{Kind: memory.Intent, Intent: intentThing, User: speechText})
}

func forkEpisode(esn string, e memory.Episode) {
	e.At, e.Lang = forkNow(), lang.Current()
	if err := memory.Capture(esn, e, forkBotZone(esn)); err != nil {
		logger.Println("myvector: memory capture:", err)
	}
}
