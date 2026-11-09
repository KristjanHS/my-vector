package main

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	completeResp = &vectorpb.ExternalAudioStreamResponse{AudioResponseType: &vectorpb.ExternalAudioStreamResponse_AudioStreamPlaybackComplete{
		AudioStreamPlaybackComplete: &vectorpb.ExternalAudioStreamPlaybackComplete{}}}
	failureResp = &vectorpb.ExternalAudioStreamResponse{AudioResponseType: &vectorpb.ExternalAudioStreamResponse_AudioStreamPlaybackFailyer{
		AudioStreamPlaybackFailyer: &vectorpb.ExternalAudioStreamPlaybackFailure{}}}
	overrunResp = &vectorpb.ExternalAudioStreamResponse{AudioResponseType: &vectorpb.ExternalAudioStreamResponse_AudioStreamBufferOverrun{
		AudioStreamBufferOverrun: &vectorpb.ExternalAudioStreamBufferOverrun{}}}
	busy = status.Error(codes.ResourceExhausted, "busy")
)

func TestVerdictAfterSkipsStaleComplete(t *testing.T) {
	sent := time.Now()
	ev := make(chan event, 2)
	ev <- event{at: sent.Add(-time.Millisecond), r: completeResp}
	ev <- event{at: sent.Add(time.Second), r: completeResp}
	if e, act, ok := verdictAfter(context.Background(), ev, sent); !ok || act != verdict || !e.at.After(sent) {
		t.Fatalf("verdict = %v %v at %v, want the PlaybackComplete after sent", ok, act, e.at)
	}
	ev <- event{at: sent, r: completeResp}
	ev <- event{err: busy}
	if e, act, ok := verdictAfter(context.Background(), ev, time.Time{}); !ok || act != retry || status.Code(e.err) != codes.ResourceExhausted {
		t.Fatalf("unsent verdict = %v %v %v, want the refusal (a Complete before ours is stale)", ok, act, e.err)
	}
}

func TestClassify(t *testing.T) {
	sent := time.Now()
	early, late := sent.Add(-time.Millisecond), sent.Add(time.Millisecond)
	for _, c := range []struct {
		name string
		e    event
		sent time.Time
		want action
	}{
		{"refusal", event{at: late, err: busy}, sent, retry},
		{"refusal unsent", event{err: busy}, time.Time{}, retry},
		{"other stream end", event{at: late, err: io.EOF}, sent, verdict},
		{"failure before Complete", event{at: early, r: failureResp}, sent, retry},
		{"failure unsent", event{at: late, r: failureResp}, time.Time{}, retry},
		{"failure after Complete", event{at: late, r: failureResp}, sent, verdict},
		{"overrun before Complete", event{at: early, r: overrunResp}, sent, retry},
		{"overrun after Complete", event{at: late, r: overrunResp}, sent, verdict},
		{"stale PlaybackComplete", event{at: early, r: completeResp}, sent, skip},
		{"PlaybackComplete unsent", event{at: late, r: completeResp}, time.Time{}, skip},
		{"PlaybackComplete after Complete", event{at: late, r: completeResp}, sent, verdict},
		{"other response", event{at: late, r: &vectorpb.ExternalAudioStreamResponse{}}, sent, skip},
	} {
		if got := classify(c.e, c.sent); got != c.want {
			t.Errorf("%s: classify = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBAdmitted(t *testing.T) {
	for _, c := range []struct {
		name    string
		ev      []event
		closed  bool
		want    bool
		wantErr bool
	}{
		{"silence", nil, false, true, false},
		{"a response", []event{{r: completeResp}}, false, true, false},
		{"refused", []event{{err: busy}}, false, false, false},
		{"other stream end", []event{{err: io.EOF}}, false, false, true},
		{"closed", nil, true, false, true},
	} {
		ch := make(chan event, 1)
		for _, e := range c.ev {
			ch <- e
		}
		if c.closed {
			close(ch)
		}
		got, err := bAdmitted(ch)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("%s: bAdmitted = %v, %v; want %v, err %v", c.name, got, err, c.want, c.wantErr)
		}
	}
}

func TestRaceMargin(t *testing.T) {
	start := time.Now()
	if m := raceMargin(start, start.Add(raceChunks*32*time.Millisecond)); m != 0 {
		t.Errorf("margin at A's audio end = %v, want 0", m)
	}
	if m := raceMargin(start, start.Add(3*time.Second)); m != -8*time.Millisecond {
		t.Errorf("margin at 3 s = %v, want -8ms (A's audio is 94 × 32 ms)", m)
	}
}

// fakeStream answers Recv with n empty responses, then io.EOF; n < 0 never ends.
type fakeStream struct {
	vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient
	n int
}

func (f *fakeStream) Recv() (*vectorpb.ExternalAudioStreamResponse, error) {
	if f.n == 0 {
		return nil, io.EOF
	}
	f.n--
	return &vectorpb.ExternalAudioStreamResponse{}, nil
}

func TestWatchForwardsPastTheBuffer(t *testing.T) {
	ch := watch(context.Background(), &fakeStream{n: 40}, "T")
	time.Sleep(50 * time.Millisecond) // the reader lags: the 16-slot buffer fills
	got := 0
	var last event
	for e := range ch {
		got++
		last = e
	}
	if got != 41 || !errors.Is(last.err, io.EOF) {
		t.Fatalf("forwarded %d events ending %v, want 41 ending io.EOF", got, last.err)
	}
}

func TestWatchEndsWithCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := watch(ctx, &fakeStream{n: -1}, "T")
	time.Sleep(20 * time.Millisecond) // unread: the buffer fills and watch blocks
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, more := <-ch:
			if !more {
				return
			}
		case <-deadline:
			t.Fatal("watch still forwarding a second after its ctx ended")
		}
	}
}
