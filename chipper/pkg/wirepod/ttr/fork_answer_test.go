package wirepod_ttr

import (
	"context"
	"errors"
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
	deny                             error // BehaviorControl fails at once
	hang                             bool  // a ControlRequest is never granted
}

func (b *controlBot) BehaviorControl(ctx context.Context, _ ...grpc.CallOption) (vectorpb.ExternalInterface_BehaviorControlClient, error) {
	if b.deny != nil {
		return nil, b.deny
	}
	return &controlStream{b: b, ctx: ctx, granted: make(chan bool, 1)}, nil
}

func (b *controlBot) note(e string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.log = append(b.log, e)
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
		if !c.b.hang {
			c.granted <- true
		}
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
		c.b.note("gone")
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

func TestForkBControlStopBeforeStart(t *testing.T) {
	for name, c := range map[string]struct {
		hang bool
		want string
	}{
		"pending": {true, "request,gone"},     // stop while the robot has not granted control
		"granted": {false, "request,release"}, // stop before the speak loop took start
	} {
		t.Run(name, func(t *testing.T) {
			b := &controlBot{hang: c.hang}
			start, stop := make(chan bool), make(chan bool)
			defer forkBControl(&vector.Vector{Conn: b}, context.Background(), start, stop, "00e3"+name)()
			if !c.hang {
				for deadline := time.Now().Add(time.Second); b.events() != "request" && time.Now().Before(deadline); {
					time.Sleep(10 * time.Millisecond)
				}
			}
			select {
			case stop <- true: // kgsim's LLM-error path, before its speak loop
			case <-time.After(time.Second):
				t.Fatal("stop blocked while the answer waited to start")
			}
			waitStartEnds(t, start)
			for deadline := time.Now().Add(time.Second); b.events() != c.want && time.Now().Before(deadline); {
				time.Sleep(10 * time.Millisecond)
			}
			if got := b.events(); got != c.want {
				t.Errorf("control = %s, want %s", got, c.want)
			}
		})
	}
}

func TestForkBControlNeverStarts(t *testing.T) {
	b := &controlBot{deny: errors.New("robot unreachable")}
	start, stop := make(chan bool), make(chan bool)
	defer forkBControl(&vector.Vector{Conn: b}, context.Background(), start, stop, "00e4")()
	waitStartEnds(t, start)
}

// waitStartEnds fails unless start closes without a send, as StreamingKGSim's `for range start` needs.
func waitStartEnds(t *testing.T, start chan bool) {
	t.Helper()
	ended := make(chan int, 1)
	go func() {
		n := 0
		for range start {
			n++
		}
		ended <- n
	}()
	select {
	case n := <-ended:
		if n != 0 {
			t.Errorf("start sent %d time(s) without control, want 0", n)
		}
	case <-time.After(time.Second):
		t.Error("start never closed: StreamingKGSim's speak loop waits forever")
	}
}
