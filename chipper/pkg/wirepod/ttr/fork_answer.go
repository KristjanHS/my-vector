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
		hold := time.NewTimer(wait)
		defer hold.Stop()
		defer close(start) // every exit ends StreamingKGSim's `for range start`; only this goroutine sends start, once
		release := forkControl(robot, ctx, stop, done, hold.C)
		if release == nil {
			return
		}
		defer release()
		select {
		case start <- true:
		case <-stop:
			return
		case <-done:
			return
		case <-hold.C:
			return
		}
		select {
		case <-stop:
		case <-done:
		case <-hold.C:
			logger.Println("myvector: answer for " + esn + " sent no stop within " + wait.String() + ", releasing")
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// forkControl is voice.Control given up on stop, done or hold; nil = no control.
func forkControl(robot *vector.Vector, ctx context.Context, stop chan bool, done chan struct{}, hold <-chan time.Time) func() {
	ctx, cancel := context.WithCancel(ctx)
	granted := make(chan func(), 1)
	go func() {
		release, err := voice.Control(ctx, robot)
		if err != nil {
			logger.Println("myvector: answer behavior control: " + err.Error())
		}
		granted <- release
	}()
	select {
	case release := <-granted:
		if release == nil {
			cancel()
			return nil
		}
		return func() { release(); cancel() }
	case <-stop:
	case <-done:
	case <-hold:
	}
	cancel()
	if release := <-granted; release != nil { // granted as it was given up: the next answer must not share it
		release()
	}
	return nil
}

// forkEndKGLoop stops kgsim's forkWait loop when start closed unsent: that loop stops only inside `for range start`.
func forkEndKGLoop(isKG bool, stopLooping *bool, ready chan bool) {
	if isKG && !*stopLooping {
		*stopLooping = true
		<-ready
	}
}

func forkDrain(stop chan bool, wait time.Duration) {
	go func() {
		select {
		case <-stop:
		case <-time.After(wait):
		}
	}()
}
