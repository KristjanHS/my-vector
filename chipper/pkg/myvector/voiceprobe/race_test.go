package main

import (
	"context"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestVerdictAfterSkipsStaleComplete(t *testing.T) {
	complete := &vectorpb.ExternalAudioStreamResponse{AudioResponseType: &vectorpb.ExternalAudioStreamResponse_AudioStreamPlaybackComplete{
		AudioStreamPlaybackComplete: &vectorpb.ExternalAudioStreamPlaybackComplete{}}}
	sent := time.Now()
	ev := make(chan event, 2)
	ev <- event{at: sent.Add(-time.Millisecond), r: complete}
	ev <- event{at: sent.Add(time.Second), r: complete}
	if e, ok := verdictAfter(context.Background(), ev, sent); !ok || !e.at.After(sent) {
		t.Fatalf("verdict = %v at %v, want the PlaybackComplete after sent", ok, e.at)
	}
	ev <- event{at: sent, r: complete}
	ev <- event{err: status.Error(codes.ResourceExhausted, "busy")}
	if e, ok := verdictAfter(context.Background(), ev, time.Time{}); !ok || status.Code(e.err) != codes.ResourceExhausted {
		t.Fatalf("unsent verdict = %v %v, want the refusal (a Complete before ours is stale)", ok, e.err)
	}
}
