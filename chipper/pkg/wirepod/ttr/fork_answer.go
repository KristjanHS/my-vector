package wirepod_ttr

import (
	"context"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
)

// forkAnswerHold frees a robot whose answer never sent stop (kgsim's hung paths); past any capped LLM answer.
var forkAnswerHold = 5 * time.Minute

var forkAnswers = struct {
	sync.Mutex
	slot map[string]chan struct{}
}{slot: map[string]chan struct{}{}}

// forkBControl is BControl behind esn's answer slot: a second answer waits here until the first released control
// (sdk-audio-orphan.md (docs/plans/)). StreamingKGSim defers the returned end, as its speak loop never returns.
func forkBControl(robot *vector.Vector, ctx context.Context, start, stop chan bool, esn string) (end func()) {
	forkAnswers.Lock()
	slot := forkAnswers.slot[esn]
	if slot == nil {
		slot = make(chan struct{}, 1)
		forkAnswers.slot[esn] = slot
	}
	forkAnswers.Unlock()
	select {
	case slot <- struct{}{}:
	default:
		logger.Println("myvector: answer for " + esn + " waits for the one speaking")
		slot <- struct{}{}
	}
	done, wait := make(chan struct{}), forkAnswerHold
	var once sync.Once
	go func() {
		defer func() { <-slot }()
		defer forkDrain(stop, wait) // a late or second stop (interrupt + speak loop) must not block its sender
		release, err := voice.Control(ctx, robot)
		if err != nil {
			logger.Println("myvector: answer behavior control: " + err.Error())
			return
		}
		defer release()
		select {
		case start <- true:
		case <-done:
			return
		}
		hold := time.NewTimer(wait)
		defer hold.Stop()
		select {
		case <-stop:
		case <-done:
		case <-hold.C:
			logger.Println("myvector: answer for " + esn + " sent no stop within " + wait.String() + ", releasing")
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

func forkDrain(stop chan bool, wait time.Duration) {
	go func() {
		select {
		case <-stop:
		case <-time.After(wait):
		}
	}()
}
