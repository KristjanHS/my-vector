package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
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

// kgsim.go's isKG branch is the one forkWait call; no non-comment line of the package plays the searching loop.
func TestForkWaitHook(t *testing.T) {
	b, err := os.ReadFile("kgsim.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n\t\tforkWait(robot, forkWaitSearch, &kgStopLooping, kgReadyToAnswer)") {
		t.Error("kgsim.go: StreamingKGSim lacks the forkWait(robot, forkWaitSearch, ...) hook")
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
