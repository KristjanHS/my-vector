package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/photo"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
	"github.com/sashabaranov/go-openai"
	"google.golang.org/grpc"
)

type waitBot struct {
	*controlBot // BehaviorControl for forkBControl; any other method panics
	mu          sync.Mutex
	plays       []string
}

func (b *waitBot) PlayAnimation(_ context.Context, in *vectorpb.PlayAnimationRequest, _ ...grpc.CallOption) (*vectorpb.PlayAnimationResponse, error) {
	b.mu.Lock()
	b.plays = append(b.plays, in.GetAnimation().GetName())
	b.mu.Unlock()
	time.Sleep(2 * time.Millisecond) // the clip's play time
	return &vectorpb.PlayAnimationResponse{}, nil
}

func (b *waitBot) played() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.plays...)
}

func (b *waitBot) waitPlays(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); len(b.played()) < n; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%d plays within 1s, want >= %d", len(b.played()), n)
		}
	}
}

func shortWaitGap(t *testing.T) {
	old := forkWaitGap
	forkWaitGap = time.Millisecond
	t.Cleanup(func() { forkWaitGap = old })
}

// assertWaitEnded: every play is the think face, and none follows the stop.
func assertWaitEnded(t *testing.T, b *waitBot) {
	t.Helper()
	n := len(b.played())
	time.Sleep(50 * forkWaitGap)
	got := b.played()
	if len(got) != n {
		t.Errorf("%d plays after the stop, want 0: the wait outlives its stop", len(got)-n)
	}
	for _, name := range got {
		if name != "anim_avs_think_loop_01" {
			t.Fatalf("wait played %q, want only anim_avs_think_loop_01 (W2-A)", name)
		}
	}
}

func receiveReady(t *testing.T, ready chan bool) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("no kgReadyToAnswer within 1s of the stop")
	}
}

// kgsim's stops before getout and before the KGSim error line: `kgStopLooping = true` + one receive.
func TestForkWaitStopsOnSpeakAndError(t *testing.T) {
	shortWaitGap(t)
	for _, kind := range []forkWaitKind{forkWaitSearch, forkWaitThink, forkWaitLook} {
		b := &waitBot{controlBot: &controlBot{}}
		stopLooping, ready := false, make(chan bool)
		forkWait(&vector.Vector{Conn: b}, kind, &stopLooping, ready)
		b.waitPlays(t, 3)
		stopLooping = true
		receiveReady(t, ready)
		assertWaitEnded(t, b)
	}
}

func TestForkWaitKindSwitch(t *testing.T) {
	shortWaitGap(t)
	b := &waitBot{controlBot: &controlBot{}}
	stopLooping, ready := false, make(chan bool)
	w := forkWait(&vector.Vector{Conn: b}, forkWaitSearch, &stopLooping, ready)
	b.waitPlays(t, 2)
	w.Set(forkWaitLook)
	b.waitPlays(t, len(b.played())+2)
	endKGLoop(t, &stopLooping, ready)
	assertWaitEnded(t, b)
}

func endKGLoop(t *testing.T, stopLooping *bool, ready chan bool) {
	t.Helper()
	done := make(chan struct{})
	go func() { forkEndKGLoop(true, stopLooping, ready); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("forkEndKGLoop blocked: the wait never sent kgReadyToAnswer")
	}
}

// An interrupt before the speak loop: istop → stop → forkBControl closes start unsent → forkEndKGLoop.
func TestForkWaitStopsOnInterrupt(t *testing.T) {
	shortWaitGap(t)
	b := &waitBot{controlBot: &controlBot{hang: true}}
	robot := &vector.Vector{Conn: b}
	start, stop := make(chan bool), make(chan bool)
	defer forkBControl(robot, context.Background(), start, stop, "00e6")()
	stopLooping, ready := false, make(chan bool)
	forkWait(robot, forkWaitSearch, &stopLooping, ready)
	_, istop, end := forkInterruptBot(robot, stop)
	defer end()
	b.waitPlays(t, 3)
	istop <- true
	for range start {
		t.Fatal("start sent after the interrupt")
	}
	endKGLoop(t, &stopLooping, ready)
	assertWaitEnded(t, b)
}

// kgsim.go's one defer forkWaitStart(...)() starts every wait; no non-comment line names the searching anim but forkWaitSearchFace.
func TestForkWaitHook(t *testing.T) {
	b, err := os.ReadFile("kgsim.go")
	if err != nil {
		t.Fatal(err)
	}
	hook := strings.Index(string(b), "\n\tdefer forkWaitStart(req, robot, ctx, start, stop, esn, transcribedText, isKG, &kgStopLooping, kgReadyToAnswer)()")
	if hook < 0 {
		t.Error("kgsim.go: StreamingKGSim lacks the defer forkWaitStart(...)() hook")
	}
	if ai := strings.Index(string(b), "\n\taireq := CreateAIReq(transcribedText, esn, false, isKG)"); ai < 0 || hook > ai {
		t.Error("kgsim.go: the wake-word intent (forkWaitStart) must go out before CreateAIReq, so a slow first token never times the robot out")
	}
	if !strings.Contains(string(b), "\n\t\t\tforkFirstToken(esn, response.Choices[0].Delta.Content)") {
		t.Error("kgsim.go: the stream loop lacks the forkFirstToken(esn, content) TTFT hook")
	}
	if strings.Contains(string(b), `IntentPass(req, "intent_greeting_hello"`) || strings.Contains(string(b), "forkBControl(") {
		t.Error("kgsim.go: a wake-word intent or control outside forkWaitStart (W6)")
	}
	files, _ := os.ReadDir(".")
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			face := f.Name() == "fork_wait.go" && strings.HasPrefix(line, "const forkWaitSearchFace = ")
			if !face && !strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, `"anim_knowledgegraph_searching_01"`) {
				t.Errorf("%s:%d plays anim_knowledgegraph_searching_01 outside forkWaitFace (loop sfx without 0022, W10)", f.Name(), i+1)
			}
		}
	}
}

// Search shows the searching face only when the robot reports MyVectorSilentWait on; think and look never do.
func TestForkWaitSearchFaceOnlyOnSilentRobot(t *testing.T) {
	shortWaitGap(t)
	old := forkSilentWaitOn
	t.Cleanup(func() { forkSilentWaitOn = old })
	for _, on := range []bool{true, false} {
		forkSilentWaitOn = func(esn string) bool { return on && esn == "00e6" }
		for _, kind := range []forkWaitKind{forkWaitSearch, forkWaitThink, forkWaitLook} {
			b := &waitBot{controlBot: &controlBot{}}
			robot := &vector.Vector{Conn: b}
			robot.Cfg.SerialNo = "00e6"
			stopLooping, ready := false, make(chan bool)
			forkWait(robot, kind, &stopLooping, ready)
			b.waitPlays(t, 3)
			stopLooping = true
			receiveReady(t, ready)
			want := "anim_avs_think_loop_01"
			if on && kind == forkWaitSearch {
				want = "anim_knowledgegraph_searching_01"
			}
			for _, name := range b.played() {
				if name != want {
					t.Fatalf("silent=%v kind=%d played %q, want only %q", on, kind, name, want)
				}
			}
		}
	}
}

// Only an exact "true" from vic-anim's console turns the searching face on; a missing var, error or timeout reads off.
func TestForkSilentWaitRead(t *testing.T) {
	old := forkSilentWaitTimeout
	forkSilentWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { forkSilentWaitTimeout = old })
	for _, c := range []struct {
		body   string
		status int
		delay  time.Duration
		want   bool
	}{
		{"true<br>\n", 200, 0, true},
		{"false<br>\n", 200, 0, false},
		{"Variable not found<br>\n", 200, 0, false},
		{"true<br>\n", 500, 0, false},
		{"true<br>\n", 200, 200 * time.Millisecond, false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/consolevarget" || r.URL.Query().Get("key") != "MyVectorSilentWait" {
				t.Errorf("console got %s, want /consolevarget?key=MyVectorSilentWait", r.URL)
			}
			time.Sleep(c.delay)
			w.WriteHeader(c.status)
			io.WriteString(w, c.body)
		}))
		if got := forkSilentWaitRead(srv.URL); got != c.want {
			t.Errorf("body %q status %d delay %v: read %v, want %v", c.body, c.status, c.delay, got, c.want)
		}
		srv.Close()
	}
	if forkSilentWaitRead("http://127.0.0.1:1") {
		t.Error("an unreachable console read on")
	}
}

func stubStreamSay(t *testing.T) chan string {
	said, old := make(chan string, 1), forkStreamSay
	forkStreamSay = func(_, text string) error { said <- text; return nil }
	t.Cleanup(func() { forkStreamSay = old })
	return said
}

func within(t *testing.T, done chan struct{}, d time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s within %s", what, d)
	}
}

// A stream error before the first sentence: the turn gets false, the wait stops, the error line is said.
func TestForkStreamFailedBeforeFirstSentence(t *testing.T) {
	shortWaitGap(t)
	said := stubStreamSay(t)
	b := &waitBot{controlBot: &controlBot{hang: true}}
	robot := &vector.Vector{Conn: b}
	start, stop := make(chan bool), make(chan bool)
	end := forkBControl(robot, context.Background(), start, stop, "00e7")
	defer end()
	stopLooping, ready := false, make(chan bool)
	forkWait(robot, forkWaitSearch, &stopLooping, ready)
	b.waitPlays(t, 3)
	successIntent, hooked := make(chan bool), make(chan struct{})
	var isDone bool
	go func() {
		forkStreamFailed(true, "00e7", nil, successIntent, &isDone, make(chan string), &stopLooping, ready, stop)
		close(hooked)
	}()
	select {
	case is := <-successIntent:
		if is {
			t.Fatal("successIntent true on a failed stream")
		}
	case <-time.After(time.Second):
		t.Fatal("the turn still waits on successIntent 1s after the stream failed")
	}
	end() // the turn's return
	within(t, hooked, 2*time.Second, "forkStreamFailed returned")
	assertWaitEnded(t, b)
	select {
	case s := <-said:
		if !strings.Contains(s, "error") {
			t.Errorf("said %q, want the LLM error line", s)
		}
	default:
		t.Error("no LLM error line said")
	}
}

// A stream error after the first sentence: the speak loop, waiting for chunk 1, ends; no successIntent send.
func TestForkStreamFailedAfterFirstSentence(t *testing.T) {
	chunks, speakReady := []string{"Hello."}, make(chan string)
	var isDone bool
	ended := make(chan bool, 1)
	go func() {
		_, ok := forkNextChunk(func() []string { return chunks }, func() bool { return isDone }, 1, speakReady)
		ended <- ok
	}()
	time.Sleep(20 * time.Millisecond) // the loop waits on speakReady
	stopLooping, hooked := false, make(chan struct{})
	go func() {
		forkStreamFailed(true, "00e8", chunks, make(chan bool), &isDone, speakReady, &stopLooping, make(chan bool), make(chan bool))
		close(hooked)
	}()
	within(t, hooked, time.Second, "forkStreamFailed returned (it must not send successIntent or stop)")
	select {
	case ok := <-ended:
		if ok {
			t.Error("speak loop got a chunk past the failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("speak loop still waits 2s after the stream failed")
	}
}

// Both kgsim.go stream-failure returns call the hook.
func TestForkStreamFailedHook(t *testing.T) {
	src, err := os.ReadFile("kgsim.go")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, "forkStreamFailed(isKG, esn, fullRespSlice, successIntent, &isDone, speakReady, &kgStopLooping, kgReadyToAnswer, stop)") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("kgsim.go calls forkStreamFailed %d times, want 2 (Recv error + choice-less chunk)", n)
	}
}

// orderBot logs each play into the control log, so one log orders intent, control and the wait.
type orderBot struct{ *waitBot }

func (b orderBot) PlayAnimation(ctx context.Context, in *vectorpb.PlayAnimationRequest, o ...grpc.CallOption) (*vectorpb.PlayAnimationResponse, error) {
	b.note("play")
	return b.waitBot.PlayAnimation(ctx, in, o...)
}

// W6: a wake-word turn sends its intent before control and the think wait; end() stops a wait the speak loop never
// reached, then releases control. A KG turn sends no intent and leaves its wait to kgsim.
func TestForkWaitStartOrder(t *testing.T) {
	shortWaitGap(t)
	oldIntent, oldGap, oldOn := forkWaitIntent, forkWaitIntentGap, forkSilentWaitOn
	forkWaitIntentGap = time.Millisecond
	forkSilentWaitOn = func(string) bool { return true } // search plays its own face, so the kind shows
	t.Cleanup(func() { forkWaitIntent, forkWaitIntentGap, forkSilentWaitOn = oldIntent, oldGap, oldOn })
	for _, isKG := range []bool{false, true} {
		b := &waitBot{controlBot: &controlBot{}}
		forkWaitIntent = func(_ interface{}, intent, _ string, _ map[string]string, _ bool) (interface{}, error) {
			b.note("intent " + intent)
			return nil, nil
		}
		robot := &vector.Vector{Conn: orderBot{b}}
		robot.Cfg.SerialNo = "w6"
		start, stop := make(chan bool), make(chan bool)
		go func() {
			for range start {
			}
		}()
		stopLooping, ready := false, make(chan bool)
		end := forkWaitStart(nil, robot, context.Background(), start, stop, "w6", "tell me a joke", isKG, &stopLooping, ready)
		b.waitPlays(t, 3)
		ev := strings.Split(b.events(), ",")
		want := "intent intent_greeting_hello"
		if isKG {
			want = "play" // or the control request: both run once forkBControl returns
			if ev[0] == "request" {
				want = "request"
			}
		}
		if ev[0] != want || strings.Count(b.events(), "intent ") != map[bool]int{false: 1, true: 0}[isKG] {
			t.Errorf("isKG %v: events %v, want one intent first on a wake-word turn and none on KG", isKG, ev)
		}
		end()
		if isKG { // kgsim's speak loop or error branch owns a KG wait's stop: a second receive would hang
			n := len(b.played())
			b.waitPlays(t, n+2)
			stopLooping = true
			receiveReady(t, ready)
			for _, got := range b.played() { // W12: a KG wait opens on think too, even on a silent robot
				if got != forkWaitAnim[forkWaitThink] {
					t.Fatalf("KG wait played %q, want only %s until a lookup", got, forkWaitAnim[forkWaitThink])
				}
			}
		} else {
			assertWaitEnded(t, b)
		}
		if forkWaitGet("w6") != nil {
			t.Errorf("isKG %v: end() left the wait registered", isKG)
		}
		for deadline := time.Now().Add(time.Second); !strings.Contains(b.events(), "release"); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("isKG %v: control not released after end(): %s", isKG, b.events())
			}
		}
	}
}

// W6: the caller's LLM-error intent_system_unmatched sends nothing on a request the wake-word intent already answered.
func TestForkIntentPassSkipsAnsweredUnmatched(t *testing.T) {
	old, dir := memory.Path, t.TempDir()
	memory.Path = func() string { return filepath.Join(dir, "myvectorMemory.json") }
	t.Cleanup(func() { memory.Path = old })
	a, b := &vtt.IntentRequest{Device: "w6"}, &vtt.IntentRequest{Device: "w6"}
	for i, c := range []struct {
		req    interface{}
		intent string
		skip   bool
	}{
		{a, "intent_greeting_hello", false},
		{a, "intent_system_unmatched", true},  // the LLM failed after the wake-word intent
		{b, "intent_system_unmatched", false}, // a new turn's first intent
		{b, "intent_greeting_hello", false},
		{nil, "intent_system_unmatched", false},
		{nil, "intent_system_unmatched", false},
	} {
		if got := forkIntentPass(c.req, "w6", c.intent, "x"); got != c.skip {
			t.Errorf("call %d (%s): skip %v, want %v", i, c.intent, got, c.skip)
		}
	}
}

// M4: a wake-word turn's intent writes no episode; its chat capture holds the turn.
func TestForkWaitStartNoIntentEpisode(t *testing.T) {
	root := useMemory(t, "")
	shortWaitGap(t)
	old := forkWaitIntentGap
	forkWaitIntentGap = time.Millisecond
	t.Cleanup(func() { forkWaitIntentGap = old })
	b := &waitBot{controlBot: &controlBot{}}
	start, stop := make(chan bool), make(chan bool)
	go func() {
		for range start {
		}
	}()
	stopLooping, ready := false, make(chan bool)
	req := &vtt.IntentGraphRequest{Device: "esn", Stream: &listenStream{}}
	forkWaitStart(req, &vector.Vector{Conn: b}, context.Background(), start, stop, "esn", "räägi nalja", false, &stopLooping, ready)()
	for _, e := range episodes(t, root) {
		if e.Kind == memory.Intent {
			t.Errorf("wake-word wait captured an intent episode %+v (M4)", e)
		}
	}
}

// F3: the first content chunk after a wait start reports its delay once; an empty (role-only/reasoning) chunk, later
// chunks and an unstarted esn report nothing.
func TestForkFirstToken(t *testing.T) {
	if _, ok := forkFirstToken("ttft-none", "Hi"); ok {
		t.Error("a chunk with no wait start reported a TTFT")
	}
	forkTurnStart.Store("ttft", time.Now().Add(-50*time.Millisecond))
	if _, ok := forkFirstToken("ttft", ""); ok {
		t.Error("an empty-content chunk reported a TTFT")
	}
	if d, ok := forkFirstToken("ttft", "Hi"); !ok || d < 50*time.Millisecond {
		t.Errorf("first content chunk: %v %v, want ≥50ms true (the empty chunk must leave the start)", d, ok)
	}
	if _, ok := forkFirstToken("ttft", " there"); ok {
		t.Error("a second chunk reported a TTFT again")
	}
}

// W12: the search face shows only while a lookup search.Route picked runs, then the wait is back on think.
func TestForkSearchingFace(t *testing.T) {
	shortWaitGap(t)
	oldSearch, oldOn := forkSearch, forkSilentWaitOn
	forkSilentWaitOn = func(string) bool { return true }
	t.Cleanup(func() { forkSearch, forkSilentWaitOn = oldSearch, oldOn })
	for _, c := range []struct {
		sentence string
		face     string
	}{
		{"search for news about semiconductors", forkWaitSearchFace},
		{"tell me a joke", forkWaitAnim[forkWaitThink]},
	} {
		b := &waitBot{controlBot: &controlBot{}}
		robot := &vector.Vector{Conn: b}
		robot.Cfg.SerialNo = "w12"
		stopLooping, ready := false, make(chan bool)
		w := forkWait(robot, forkWaitThink, &stopLooping, ready)
		forkWaitPut("w12", w)
		b.waitPlays(t, 2)
		var during []string
		forkSearch = func(string, []string, string, string, *time.Location) string {
			n := len(b.played())
			b.waitPlays(t, n+3)
			during = b.played()[n+1:] // the play running at the switch may still be the old face
			return ""
		}
		forkSearching("w12", c.sentence, lang.English, "claude", forkPrevAsks("w12"))
		n := len(b.played())
		b.waitPlays(t, n+3)
		after := b.played()[n+1:]
		stopLooping = true
		receiveReady(t, ready)
		forkWaitDrop("w12", w)
		for _, got := range during {
			if got != c.face {
				t.Errorf("%q: played %q during the lookup, want %s", c.sentence, got, c.face)
			}
		}
		for _, got := range after {
			if got != forkWaitAnim[forkWaitThink] {
				t.Errorf("%q: played %q after the lookup, want think", c.sentence, got)
			}
		}
	}
}

// W12: a photo look wait plays only after the shutter, ends on the first chunk (forkPhotoActs), before
// forkVisionDown's line, and on End; an ended look never starts, and a nil look is a no-op.
func TestForkLookWait(t *testing.T) {
	shortWaitGap(t)
	oldSay := forkVisionSay
	t.Cleanup(func() { forkVisionSay = oldSay })
	for _, endBy := range []string{"forkPhotoActs", "forkVisionDown", "End"} {
		b := &waitBot{controlBot: &controlBot{}}
		robot := &vector.Vector{Conn: b}
		look := forkLookFor(robot)
		shutter := make(chan struct{})
		look.Start(shutter)
		time.Sleep(20 * forkWaitGap)
		if n := len(b.played()); n != 0 {
			t.Fatalf("%s: %d plays before the shutter ended, want 0", endBy, n)
		}
		close(shutter)
		b.waitPlays(t, 2)
		forkVisionSay = func(string, *vector.Vector) error { return nil }
		done := make(chan struct{})
		go func() {
			switch endBy {
			case "forkPhotoActs":
				forkPhotoActs(nil, look)
			case "forkVisionDown":
				forkVisionDown(robot, look)
			default:
				look.End()
			}
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s did not end the look wait within 1s", endBy)
		}
		assertWaitEnded(t, b)
		look.End() // a second End returns at once
		var none *forkLook
		none.End()
		none.Start(nil)
		assertWaitEnded(t, b)
	}
	b := &waitBot{controlBot: &controlBot{}}
	early := forkLookFor(&vector.Vector{Conn: b})
	early.End() // DoGetImage returned (stopped mid-countdown) before any capture
	early.Start(nil)
	time.Sleep(20 * forkWaitGap)
	if n := len(b.played()); n != 0 {
		t.Errorf("a look ended before Start played %d clips, want 0", n)
	}
}

// blockBot's PlayAnimation runs until release closes: a clip in flight.
type blockBot struct {
	*waitBot
	playing chan struct{}
	release chan struct{}
}

func (b *blockBot) PlayAnimation(ctx context.Context, in *vectorpb.PlayAnimationRequest, o ...grpc.CallOption) (*vectorpb.PlayAnimationResponse, error) {
	select {
	case b.playing <- struct{}{}:
	default:
	}
	<-b.release
	return b.waitBot.PlayAnimation(ctx, in, o...)
}

// W12: forkPhotoActs and forkVisionDown return / speak only after the in-flight look clip ended, so neither the
// first vision sentence nor the fallback line talks over it.
func TestForkLookEndWaitsForClip(t *testing.T) {
	shortWaitGap(t)
	oldSay := forkVisionSay
	t.Cleanup(func() { forkVisionSay = oldSay })
	for _, endBy := range []string{"forkPhotoActs", "forkVisionDown"} {
		b := &blockBot{waitBot: &waitBot{controlBot: &controlBot{}}, playing: make(chan struct{}, 1), release: make(chan struct{})}
		robot := &vector.Vector{Conn: b}
		look := forkLookFor(robot)
		look.Start(nil)
		<-b.playing
		spoke, done := make(chan struct{}), make(chan struct{})
		forkVisionSay = func(string, *vector.Vector) error { close(spoke); return nil }
		go func() {
			if endBy == "forkPhotoActs" {
				forkPhotoActs(nil, look)
				close(spoke)
			} else {
				forkVisionDown(robot, look)
			}
			close(done)
		}()
		select {
		case <-spoke:
			t.Fatalf("%s spoke while the look clip was still playing", endBy)
		case <-time.After(30 * time.Millisecond):
		}
		close(b.release)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s did not return within 1s of the clip ending", endBy)
		}
	}
}

// W12: a kept-photo ask starts the look wait at capture (no shutter); DoGetImage's End stops it.
func TestForkPhotoCaptureKeptStartsLook(t *testing.T) {
	shortWaitGap(t)
	b := &waitBot{controlBot: &controlBot{}}
	robot := &vector.Vector{Conn: b}
	robot.Cfg.SerialNo = "w12kept"
	oldPhotos := forkPhotos
	forkPhotos = new(photo.Store)
	t.Cleanup(func() { forkPhotos = oldPhotos })
	forkPhotos.Put("w12kept", []byte{0xff, 0xd8}, lang.Current(), forkNow())
	forkPhotos.Answered("w12kept", forkNow())
	look := forkLookFor(robot)
	stop := false
	if resp, ok := forkPhotoCapture(robot, forkPhotoKept, &stop, look); !ok || len(resp.GetData()) == 0 {
		t.Fatalf("kept capture = %v, %v", resp, ok)
	}
	b.waitPlays(t, 2)
	look.End()
	assertWaitEnded(t, b)
}

func TestForkPrevAsks(t *testing.T) {
	old := vars.RememberedChats
	t.Cleanup(func() { vars.RememberedChats = old })
	msg := func(role, s string) openai.ChatCompletionMessage {
		return openai.ChatCompletionMessage{Role: role, Content: s}
	}
	u, a := openai.ChatMessageRoleUser, openai.ChatMessageRoleAssistant
	vars.RememberedChats = []vars.RememberedChat{{ESN: "p1", Chats: []openai.ChatCompletionMessage{
		msg(u, "one"), msg(a, "r1"), msg(u, "two"), msg(a, "r2"), msg(u, "three"), msg(a, "r3")}}}
	if got := forkPrevAsks("p1"); !reflect.DeepEqual(got, []string{"three", "two"}) {
		t.Errorf("forkPrevAsks = %q, want the last two user asks, newest first", got)
	}
	var seen []string
	oldSearch := forkSearch
	t.Cleanup(func() { forkSearch = oldSearch })
	forkSearch = func(_ string, prev []string, _, _ string, _ *time.Location) string { seen = prev; return "" }
	forkSearching("p1", "otsi seda", "et-EE", "ollama", forkPrevAsks("p1"))
	if !reflect.DeepEqual(seen, []string{"three", "two"}) {
		t.Errorf("forkSearching passed %q, want forkPrevAsks", seen)
	}
}
