package sdkapp

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
)

type fakeStream struct {
	sent      chan *vectorpb.BehaviorControlRequest
	recvs     chan recvResult
	closed    chan struct{}
	closeOnce sync.Once
}

type recvResult struct {
	resp *vectorpb.BehaviorControlResponse
	err  error
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		sent:   make(chan *vectorpb.BehaviorControlRequest, 4),
		recvs:  make(chan recvResult, 4),
		closed: make(chan struct{}),
	}
}

func (f *fakeStream) Send(r *vectorpb.BehaviorControlRequest) error { f.sent <- r; return nil }
func (f *fakeStream) Recv() (*vectorpb.BehaviorControlResponse, error) {
	r := <-f.recvs
	return r.resp, r.err
}

func granted() recvResult {
	return recvResult{resp: &vectorpb.BehaviorControlResponse{
		ResponseType: &vectorpb.BehaviorControlResponse_ControlGrantedResponse{
			ControlGrantedResponse: &vectorpb.ControlGrantedResponse{},
		},
	}}
}

func lost() recvResult {
	return recvResult{resp: &vectorpb.BehaviorControlResponse{
		ResponseType: &vectorpb.BehaviorControlResponse_ControlLostEvent{
			ControlLostEvent: &vectorpb.ControlLostResponse{},
		},
	}}
}

func reservedLost() recvResult {
	return recvResult{resp: &vectorpb.BehaviorControlResponse{
		ResponseType: &vectorpb.BehaviorControlResponse_ReservedControlLostEvent{
			ReservedControlLostEvent: &vectorpb.ReservedControlLostResponse{},
		},
	}}
}

// opener hands out the given streams in order, then returns errors; each open is reported on opened.
func opener(streams ...*fakeStream) (holdOpener, chan string) {
	opened := make(chan string, 64)
	var mu sync.Mutex
	next := 0
	return func(esn string) (holdStream, func(), error) {
		mu.Lock()
		defer mu.Unlock()
		opened <- esn
		if next >= len(streams) {
			return nil, nil, errors.New("no more streams")
		}
		s := streams[next]
		next++
		return s, func() { s.closeOnce.Do(func() { close(s.closed) }) }, nil
	}, opened
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

func recvWithin[T any](t *testing.T, c <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not happen within 2s", what)
	}
	var zero T
	return zero
}

func TestAutoHoldPriority(t *testing.T) {
	cases := map[string]struct {
		prio vectorpb.ControlRequest_Priority
		ok   bool
	}{
		"override": {vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, true},
		" Reserve": {vectorpb.ControlRequest_RESERVE_CONTROL, true},
		"off":      {0, false},
		"":         {0, false},
		"high":     {0, false},
	}
	for mode, want := range cases {
		prio, ok := autoHoldPriority(mode)
		if ok != want.ok || (ok && prio != want.prio) {
			t.Errorf("autoHoldPriority(%q) = %v,%v; want %v,%v", mode, prio, ok, want.prio, want.ok)
		}
	}
}

func TestAutoHoldRequestsPriorityAndMarksGranted(t *testing.T) {
	s := newFakeStream()
	open, _ := opener(s)
	h := newAutoHolder(vectorpb.ControlRequest_RESERVE_CONTROL, time.Millisecond, time.Millisecond, open, func(string) {})
	defer close(h.quit)

	h.scan([]string{"00e20145"})
	req := recvWithin(t, s.sent, "control request")
	if got := req.GetControlRequest().GetPriority(); got != vectorpb.ControlRequest_RESERVE_CONTROL {
		t.Fatalf("requested priority %v, want RESERVE_CONTROL", got)
	}
	if h.isGranted("00e20145") {
		t.Fatal("granted before ControlGrantedResponse")
	}
	s.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
}

func TestAutoHoldReassumesAndClosesAfterStreamErrorAndControlLost(t *testing.T) {
	streams := []*fakeStream{newFakeStream(), newFakeStream(), newFakeStream(), newFakeStream()}
	open, opened := opener(streams...)
	h := newAutoHolder(vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, time.Millisecond, time.Millisecond, open, func(string) {})
	defer close(h.quit)

	h.scan([]string{"00e20145"})
	endings := []recvResult{{err: errors.New("stream reset")}, lost(), reservedLost()}
	for i, end := range endings {
		recvWithin(t, opened, "open")
		recvWithin(t, streams[i].sent, "control request")
		streams[i].recvs <- granted()
		waitFor(t, func() bool { return h.isGranted("00e20145") })
		streams[i].recvs <- end
		recvWithin(t, streams[i].closed, "stream close")
		recvWithin(t, opened, "re-open")
		if h.isGranted("00e20145") {
			t.Fatalf("ending %d: still marked granted after the stream ended", i)
		}
		// hand the re-open back to the next iteration's wait
		opened <- "00e20145"
	}
}

func TestAutoHoldBacksOffWhileOpenFails(t *testing.T) {
	open, opened := opener() // every open fails
	h := newAutoHolder(vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, 10*time.Millisecond, 40*time.Millisecond, open, func(string) {})
	defer close(h.quit)

	h.scan([]string{"00e20145"})
	time.Sleep(300 * time.Millisecond)
	// fixed 10ms retries would open ~30 times; 10,20,40,40,... backoff opens ~8
	if n := len(opened); n > 12 {
		t.Fatalf("opened %d times in 300ms; want backoff to cap retries near 8", n)
	}
}

func TestAutoHoldScanStartsOneLoopPerRobotAndTouchesHeld(t *testing.T) {
	s := newFakeStream()
	open, opened := opener(s)
	var mu sync.Mutex
	touched := map[string]int{}
	h := newAutoHolder(vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, time.Hour, time.Hour, open, func(esn string) {
		mu.Lock()
		touched[esn]++
		mu.Unlock()
	})
	defer close(h.quit)

	h.scan([]string{"00E20145"})
	if got := recvWithin(t, opened, "open"); got != "00e20145" {
		t.Fatalf("opened %q, want lower-cased esn", got)
	}
	h.scan([]string{"00e20145"})
	select {
	case esn := <-opened:
		t.Fatalf("second scan opened another stream for %q", esn)
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	if touched["00e20145"] != 0 {
		t.Fatal("touched before control was granted")
	}
	mu.Unlock()

	s.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
	h.scan([]string{"00e20145"})
	mu.Lock()
	defer mu.Unlock()
	if touched["00e20145"] != 1 {
		t.Fatalf("touched %d times, want 1", touched["00e20145"])
	}
}
