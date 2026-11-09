package wirepod_ttr

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
)

type controlBot struct {
	vectorpb.ExternalInterfaceClient // nil: any other method panics
	mu                               sync.Mutex
	log                              []string
}

func (b *controlBot) BehaviorControl(ctx context.Context, _ ...grpc.CallOption) (vectorpb.ExternalInterface_BehaviorControlClient, error) {
	return &controlStream{b: b, ctx: ctx, granted: make(chan bool, 1)}, nil
}

func (b *controlBot) events() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.log, ",")
}

type controlStream struct {
	grpc.ClientStream
	b       *controlBot
	ctx     context.Context
	granted chan bool
}

func (c *controlStream) Send(m *vectorpb.BehaviorControlRequest) error {
	c.b.mu.Lock()
	defer c.b.mu.Unlock()
	if m.GetControlRequest() != nil {
		c.b.log = append(c.b.log, "request")
		c.granted <- true
	} else if m.GetControlRelease() != nil {
		c.b.log = append(c.b.log, "release")
	}
	return nil
}

func (c *controlStream) Recv() (*vectorpb.BehaviorControlResponse, error) {
	select {
	case <-c.granted:
		return &vectorpb.BehaviorControlResponse{ResponseType: &vectorpb.BehaviorControlResponse_ControlGrantedResponse{
			ControlGrantedResponse: &vectorpb.ControlGrantedResponse{}}}, nil
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
}

func TestForkBControlQueuesAnswers(t *testing.T) {
	old := forkAnswerHold
	forkAnswerHold = 300 * time.Millisecond
	t.Cleanup(func() { forkAnswerHold = old })
	for name, endFirst := range map[string]func(stop chan bool, end func()){
		"stop":   func(stop chan bool, _ func()) { stop <- true }, // the speak loop's end, or an interrupt
		"return": func(_ chan bool, end func()) { end() },         // StreamingKGSim returned without stop
		"hold":   func(chan bool, func()) {},                      // a kgsim path that never sends stop
	} {
		t.Run(name, func(t *testing.T) {
			b := &controlBot{}
			robot := &vector.Vector{Conn: b}
			ctx := context.Background()
			start1, stop1 := make(chan bool), make(chan bool)
			end1 := forkBControl(robot, ctx, start1, stop1, "00e2")
			defer end1()
			<-start1
			start2, stop2 := make(chan bool), make(chan bool)
			second := make(chan func(), 1)
			go func() { second <- forkBControl(robot, ctx, start2, stop2, "00e2") }()
			select {
			case end2 := <-second:
				end2()
				t.Fatal("the second answer took control while the first held it")
			case <-time.After(100 * time.Millisecond):
			}
			endFirst(stop1, end1)
			select {
			case end2 := <-second:
				defer end2()
			case <-time.After(2 * forkAnswerHold):
				t.Fatal("the second answer still waits after the first ended")
			}
			<-start2
			if got := b.events(); got != "request,release,request" {
				t.Errorf("control = %s, want request,release,request (the first released before the second asked)", got)
			}
		})
	}
}
