package wirepod_ttr

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
)

type eventBot struct {
	vectorpb.ExternalInterfaceClient // nil: any other method panics
	recvErr                          chan error
}

func (b *eventBot) EventStream(ctx context.Context, _ *vectorpb.EventRequest, _ ...grpc.CallOption) (vectorpb.ExternalInterface_EventStreamClient, error) {
	return &eventStream{ctx: ctx, recvErr: b.recvErr}, nil
}

type eventStream struct {
	vectorpb.ExternalInterface_EventStreamClient
	ctx     context.Context
	recvErr chan error
}

func (s *eventStream) Recv() (*vectorpb.EventResponse, error) {
	<-s.ctx.Done()
	s.recvErr <- s.ctx.Err()
	return nil, s.ctx.Err()
}

func TestForkInterruptEnds(t *testing.T) {
	base := runtime.NumGoroutine()
	fake := &eventBot{recvErr: make(chan error, 1)}
	bot, end := forkInterruptBot(&vector.Vector{Conn: fake})
	stop, stopStop := make(chan bool), make(chan bool, 1)
	done := make(chan bool)
	go func() { done <- InterruptKGSimWhenTouchedOrWaked(bot, stop, stopStop) }()

	select {
	case <-done:
		t.Fatal("interrupter returned before end")
	case <-time.After(50 * time.Millisecond):
	}
	end()
	close(stopStop)
	select {
	case err := <-fake.recvErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Recv err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EventStream outlived end")
	}
	if <-done {
		t.Fatal("interrupter reported an interrupt")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		t.Fatalf("goroutines = %d, baseline %d", n, base)
	}
}
