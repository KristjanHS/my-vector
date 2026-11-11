package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
)

type forkWaitKind int32

const (
	forkWaitSearch forkWaitKind = iota // the KG wait (kgsim's isKG branch)
	forkWaitThink
	forkWaitLook
)

// forkWaitAnim is each kind's face (W3): think (−99 dBFS); search plays forkWaitSearchFace only on a silent robot.
var forkWaitAnim = map[forkWaitKind]string{
	forkWaitSearch: "anim_avs_think_loop_01",
	forkWaitThink:  "anim_avs_think_loop_01",
	forkWaitLook:   "anim_avs_think_loop_01",
}

// forkWaitSearchFace's clip starts a loop sfx only its getout stops (W10); firmware 0022 drops it while MyVectorSilentWait.
const forkWaitSearchFace = "anim_knowledgegraph_searching_01"

func forkWaitFace(kind forkWaitKind, silent bool) string {
	if kind == forkWaitSearch && silent {
		return forkWaitSearchFace
	}
	return forkWaitAnim[kind]
}

var forkSilentWaitTimeout = 500 * time.Millisecond

// forkSilentWaitOn: the robot's vic-anim console reads MyVectorSilentWait "true" (0022); any miss or error = false.
var forkSilentWaitOn = func(esn string) bool {
	ip := forkRobotIP(esn)
	return ip != "" && forkSilentWaitRead(listen.ConsoleURL(ip))
}

func forkSilentWaitRead(console string) bool {
	c := http.Client{Timeout: forkSilentWaitTimeout}
	r, err := c.Get(console + "/consolevarget?key=MyVectorSilentWait")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 64))
	return err == nil && r.StatusCode == http.StatusOK && strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(b)), "<br>")) == "true"
}

var forkWaitGap = time.Second / 3 // upstream's pause between two plays of the KG loop

type forkWaiter struct{ kind int32 }

// Set switches the face from the next play on (W4).
func (w *forkWaiter) Set(k forkWaitKind) { atomic.StoreInt32(&w.kind, int32(k)) }

// forkWait plays kind's face until *stopLooping, then sends ready once: stop = kgsim's
// `kgStopLooping = true` + receive from kgReadyToAnswer, or forkEndKGLoop.
func forkWait(robot *vector.Vector, kind forkWaitKind, stopLooping *bool, ready chan bool) *forkWaiter {
	w := &forkWaiter{}
	w.Set(kind)
	go func() {
		silent := robot.Cfg.SerialNo != "" && forkSilentWaitOn(robot.Cfg.SerialNo)
		for !*stopLooping {
			robot.Conn.PlayAnimation(context.Background(), &vectorpb.PlayAnimationRequest{
				Animation: &vectorpb.Animation{Name: forkWaitFace(forkWaitKind(atomic.LoadInt32(&w.kind)), silent)},
				Loops:     1,
			})
			time.Sleep(forkWaitGap)
		}
		ready <- true
	}()
	return w
}

// forkStreamSay speaks the stream-failure line (upstream's empty-EOF KGSim); tests stub it.
var forkStreamSay = KGSim

// forkStreamFailed is StreamingKGSim's non-EOF Recv error / choice-less chunk hook (W4): upstream returned with the
// turn on `range successIntent` or the speak loop on isDone, and forkWait looping. Mirrors the empty-EOF branch.
func forkStreamFailed(isKG bool, esn string, chunks []string, successIntent chan bool, isDone *bool, speakReady chan string, stopLooping *bool, ready chan bool, stop chan bool) {
	if len(chunks) > 0 { // the turn took a sentence: the speak loop ends the wait and the turn
		*isDone = true
		new(forkRepeats).wake(speakReady)
		return
	}
	successIntent <- false
	if isKG {
		forkEndKGLoop(isKG, stopLooping, ready)
		stop <- true
		time.Sleep(time.Second / 3)
		forkStreamSay(esn, "There was an error getting data from the L. L. M.")
	}
}
