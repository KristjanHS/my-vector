package wirepod_ttr

import (
	"context"
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
			if !strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, `"anim_knowledgegraph_searching_01"`) {
				t.Errorf("%s:%d plays anim_knowledgegraph_searching_01 (its loop sfx outlives the wait, W10)", f.Name(), i+1)
			}
		}
	}
}
