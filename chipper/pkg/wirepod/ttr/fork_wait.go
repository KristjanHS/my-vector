package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/sashabaranov/go-openai"
)

type forkWaitKind int32

const (
	forkWaitSearch forkWaitKind = iota // while forkSearching's lookup runs
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
	go w.loop(robot, stopLooping, ready)
	return w
}

func (w *forkWaiter) loop(robot *vector.Vector, stopLooping *bool, ready chan bool) {
	silent := robot.Cfg.SerialNo != "" && forkSilentWaitOn(robot.Cfg.SerialNo)
	for !*stopLooping {
		robot.Conn.PlayAnimation(context.Background(), &vectorpb.PlayAnimationRequest{
			Animation: &vectorpb.Animation{Name: forkWaitFace(forkWaitKind(atomic.LoadInt32(&w.kind)), silent)},
			Loops:     1,
		})
		time.Sleep(forkWaitGap)
	}
	ready <- true
}

// forkWaits is each robot's running StreamingKGSim wait (W12), so a fetch that knows only the esn switches its face.
var forkWaits = struct {
	sync.Mutex
	m map[string]*forkWaiter
}{m: map[string]*forkWaiter{}}

func forkWaitPut(esn string, w *forkWaiter) {
	forkWaits.Lock()
	forkWaits.m[esn] = w
	forkWaits.Unlock()
}

// forkWaitDrop forgets w, unless a later wait already replaced it.
func forkWaitDrop(esn string, w *forkWaiter) {
	forkWaits.Lock()
	if forkWaits.m[esn] == w {
		delete(forkWaits.m, esn)
	}
	forkWaits.Unlock()
}

func forkWaitGet(esn string) *forkWaiter {
	forkWaits.Lock()
	defer forkWaits.Unlock()
	return forkWaits.m[esn]
}

// forkSearching is forkAIReq's forkSearch under the search face (W12): only a sentence search.Route sends to a
// lookup shows it, and the wait is back on think once the lookup returns.
func forkSearching(esn, last, l, brain string, prev []string) string {
	if w := forkWaitGet(esn); w != nil && search.Route(last, l) != search.None {
		w.Set(forkWaitSearch) // one waiter both ways: a later turn's wait is never the one set back
		defer w.Set(forkWaitThink)
	}
	return forkSearch(last, prev, l, brain, forkBotZone(esn))
}

// forkPrevAsks is the robot's last two remembered user asks, newest first (search-trigger-fixes R7).
func forkPrevAsks(esn string) []string {
	var out []string
	chats := GetChat(esn).Chats
	for i := len(chats) - 1; i >= 0 && len(out) < 2; i-- {
		if chats[i].Role == openai.ChatMessageRoleUser {
			out = append(out, chats[i].Content)
		}
	}
	return out
}

// forkLookEndMax bounds End's wait for the last clip (a robot gone mid-clip); the loop still stops after it.
var forkLookEndMax = 3 * time.Second

// forkLook is one DoGetImage's look wait (W12), owned by that call so a later turn's wait is never the one it ends:
// Start plays the look face after closes (nil = now); End stops it and returns once its last clip ended.
type forkLook struct {
	robot         *vector.Vector
	mu            sync.Mutex
	started, stop bool
	ready         chan bool
	once          sync.Once
	done          chan struct{}
}

func forkLookFor(robot *vector.Vector) *forkLook {
	return &forkLook{robot: robot, ready: make(chan bool, 1), done: make(chan struct{})}
}

// Start begins the wait once (nil look = no-op); after End its loop sees stop and plays nothing.
func (l *forkLook) Start(after <-chan struct{}) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started {
		return
	}
	l.started = true
	w := &forkWaiter{}
	w.Set(forkWaitLook)
	go func() {
		if after != nil {
			<-after
		}
		w.loop(l.robot, &l.stop, l.ready)
	}()
}

// End is nil-safe and idempotent; every caller returns only after the first one saw the last clip end.
func (l *forkLook) End() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.mu.Lock()
		l.stop = true
		started := l.started
		l.mu.Unlock()
		if started {
			select {
			case <-l.ready:
			case <-time.After(forkLookEndMax):
			}
		}
		close(l.done)
	})
	<-l.done
}

// forkWaitIntent answers a wake-word turn's intent request; a var so tests fake it.
var forkWaitIntent = IntentPass

var forkWaitIntentGap = 200 * time.Millisecond // upstream's pause between the intent and control

// forkTurnStart holds each esn's wait start until forkFirstToken logs the turn's time to the first content chunk.
var forkTurnStart sync.Map

// forkFirstToken is the stream loop's per-chunk hook: the first chunk with content after a wait start logs and returns
// its delay; a role-only or reasoning chunk (empty content) leaves the start in place.
func forkFirstToken(esn, content string) (time.Duration, bool) {
	if content == "" {
		return 0, false
	}
	t, ok := forkTurnStart.LoadAndDelete(esn)
	if !ok {
		return 0, false
	}
	d := time.Since(t.(time.Time))
	logger.Println("myvector llm: first token " + strconv.FormatInt(d.Milliseconds(), 10) + "ms after the wait start")
	return d, true
}

// forkWaitStart is StreamingKGSim's wait (W6): a wake-word turn answers its intent first. Both kinds open on the
// think face; forkSearching shows the search face while a lookup runs (W12).
// end stops a wake-word wait the speak loop never reached (an LLM error), then releases control.
func forkWaitStart(req interface{}, robot *vector.Vector, ctx context.Context, start, stop chan bool, esn, text string, isKG bool, stopLooping *bool, ready chan bool) (end func()) {
	forkTurnStart.Store(esn, time.Now())
	if !isKG {
		memory.MarkLLM(esn, text) // M4: the chat capture holds this turn, so its intent writes no episode
		if _, err := forkWaitIntent(req, "intent_greeting_hello", text, map[string]string{}, false); err != nil {
			logger.Println("myvector wait: intent_greeting_hello: " + err.Error())
		}
		time.Sleep(forkWaitIntentGap)
	}
	release := forkBControl(robot, ctx, start, stop, esn)
	w := forkWait(robot, forkWaitThink, stopLooping, ready)
	forkWaitPut(esn, w)
	return func() {
		forkEndKGLoop(!isKG, stopLooping, ready)
		forkWaitDrop(esn, w)
		release()
	}
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
