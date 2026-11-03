package sdkapp

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
)

type fakeStream struct {
	sent  chan *vectorpb.BehaviorControlRequest
	recvs chan recvResult
}

type recvResult struct {
	resp *vectorpb.BehaviorControlResponse
	err  error
}

func newFakeStream() *fakeStream {
	return &fakeStream{sent: make(chan *vectorpb.BehaviorControlRequest, 4), recvs: make(chan recvResult, 4)}
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

// opener hands out the given streams in order, then blocks until the test ends.
func opener(t *testing.T, streams ...*fakeStream) (func(string) (holdStream, error), chan string) {
	opened := make(chan string, 8)
	var mu sync.Mutex
	next := 0
	return func(esn string) (holdStream, error) {
		mu.Lock()
		defer mu.Unlock()
		opened <- esn
		if next >= len(streams) {
			return nil, errors.New("no more streams")
		}
		s := streams[next]
		next++
		return s, nil
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
	open, _ := opener(t, s)
	h := newAutoHolder(vectorpb.ControlRequest_RESERVE_CONTROL, time.Millisecond, open, func(string) {})
	defer close(h.quit)

	h.scan([]string{"00e20145"})
	req := <-s.sent
	if got := req.GetControlRequest().GetPriority(); got != vectorpb.ControlRequest_RESERVE_CONTROL {
		t.Fatalf("requested priority %v, want RESERVE_CONTROL", got)
	}
	if h.isGranted("00e20145") {
		t.Fatal("granted before ControlGrantedResponse")
	}
	s.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
}

func TestAutoHoldReassumesAfterStreamErrorAndControlLost(t *testing.T) {
	first, second, third := newFakeStream(), newFakeStream(), newFakeStream()
	open, opened := opener(t, first, second, third)
	h := newAutoHolder(vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, time.Millisecond, open, func(string) {})
	defer close(h.quit)

	h.scan([]string{"00e20145"})
	<-opened
	first.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
	first.recvs <- recvResult{err: errors.New("stream reset")}

	<-opened
	<-second.sent
	second.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
	second.recvs <- lost()

	<-opened
	third.recvs <- granted()
	waitFor(t, func() bool { return h.isGranted("00e20145") })
}

func TestAutoHoldScanStartsOneLoopPerRobotAndTouchesHeld(t *testing.T) {
	s := newFakeStream()
	open, opened := opener(t, s)
	var mu sync.Mutex
	touched := map[string]int{}
	h := newAutoHolder(vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, time.Hour, open, func(esn string) {
		mu.Lock()
		touched[esn]++
		mu.Unlock()
	})
	defer close(h.quit)

	h.scan([]string{"00E20145"})
	if got := <-opened; got != "00e20145" {
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
