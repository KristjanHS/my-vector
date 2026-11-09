package wirepod_ttr

import (
	"context"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
)

// forkInterruptBot is robot whose EventStream ends when end is called; the interrupter opens it on context.Background().
func forkInterruptBot(robot *vector.Vector) (bot *vector.Vector, end func()) {
	ctx, cancel := context.WithCancel(context.Background())
	cp := *robot
	cp.Conn = interruptConn{robot.Conn, ctx}
	return &cp, cancel
}

type interruptConn struct {
	vectorpb.ExternalInterfaceClient
	ctx context.Context
}

func (c interruptConn) EventStream(_ context.Context, in *vectorpb.EventRequest, opts ...grpc.CallOption) (vectorpb.ExternalInterface_EventStreamClient, error) {
	return c.ExternalInterfaceClient.EventStream(c.ctx, in, opts...)
}
