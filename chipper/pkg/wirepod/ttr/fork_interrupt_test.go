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
	stop, stopStop := make(chan bool), make(chan bool, 1)
	bot, istop, end := forkInterruptBot(&vector.Vector{Conn: fake}, stop)
	done := make(chan bool)
	go func() { done <- InterruptKGSimWhenTouchedOrWaked(bot, istop, stopStop) }()

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

func TestForkInterruptCutsSpeech(t *testing.T) {
	robot := &vector.Vector{}
	stop := make(chan bool)
	_, istop, end := forkInterruptBot(robot, stop)
	defer end()
	speak := forkSpeakCtx(robot)
	if speak.Err() != nil {
		t.Fatal("speech cut before an interrupt")
	}
	istop <- true
	select {
	case <-stop:
	case <-time.After(2 * time.Second):
		t.Fatal("interrupt never reached stop")
	}
	if !errors.Is(speak.Err(), context.Canceled) {
		t.Fatalf("speak ctx err = %v after interrupt, want context.Canceled", speak.Err())
	}
	end()
	if forkSpeakCtx(robot).Err() != nil {
		t.Fatal("speak ctx still cut after the answer ended")
	}
}
