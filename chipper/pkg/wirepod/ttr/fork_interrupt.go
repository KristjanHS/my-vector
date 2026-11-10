package wirepod_ttr

import (
	"context"
	"sync"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
)

var forkSpeaking = struct {
	sync.Mutex
	ctx map[*vector.Vector]context.Context
}{ctx: map[*vector.Vector]context.Context{}}

// forkInterruptBot is robot whose EventStream ends when end is called (the interrupter opens it on context.Background()),
// and istop the interrupter's stop: a send cuts robot's speech (forkSpeakCtx) before it reaches stop.
func forkInterruptBot(robot *vector.Vector, stop chan bool) (bot *vector.Vector, istop chan bool, end func()) {
	ctx, cancel := context.WithCancel(context.Background())
	speak, cut := context.WithCancel(context.Background())
	forkSpeaking.Lock()
	forkSpeaking.ctx[robot] = speak
	forkSpeaking.Unlock()
	istop = make(chan bool, 1) // the interrupter's one send never blocks once end stopped the forwarder
	go func() {
		select {
		case <-istop:
			cut()
			select {
			case stop <- true:
			case <-ctx.Done():
			}
		case <-ctx.Done():
		}
	}()
	cp := *robot
	cp.Conn = interruptConn{robot.Conn, ctx}
	return &cp, istop, func() {
		cancel()
		cut()
		forkSpeaking.Lock()
		if forkSpeaking.ctx[robot] == speak {
			delete(forkSpeaking.ctx, robot)
		}
		forkSpeaking.Unlock()
	}
}

// forkSpeakCtx is the ctx robot's answer speaks on: cancelled by an interrupt, Background outside an answer.
func forkSpeakCtx(robot *vector.Vector) context.Context {
	forkSpeaking.Lock()
	defer forkSpeaking.Unlock()
	if ctx, ok := forkSpeaking.ctx[robot]; ok {
		return ctx
	}
	return context.Background()
}

type interruptConn struct {
	vectorpb.ExternalInterfaceClient
	ctx context.Context
}

func (c interruptConn) EventStream(_ context.Context, in *vectorpb.EventRequest, opts ...grpc.CallOption) (vectorpb.ExternalInterface_EventStreamClient, error) {
	return c.ExternalInterfaceClient.EventStream(c.ctx, in, opts...)
}
