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

func TestForkBControlStartEndsAfterAnswer(t *testing.T) {
	old := forkAnswerHold
	forkAnswerHold = 200 * time.Millisecond
	t.Cleanup(func() { forkAnswerHold = old })
	for name, endAnswer := range map[string]func(stop chan bool, end func()){
		"stop":   func(stop chan bool, _ func()) { stop <- true },
		"return": func(_ chan bool, end func()) { end() },
		"hold":   func(chan bool, func()) {},
	} {
		t.Run(name, func(t *testing.T) {
			start, stop := make(chan bool), make(chan bool)
			end := forkBControl(&vector.Vector{Conn: &controlBot{}}, context.Background(), start, stop, "00e5"+name)
			defer end()
			<-start
			endAnswer(stop, end)
			waitStartEnds(t, start)
		})
	}
}

func TestForkEndKGLoop(t *testing.T) {
	for name, c := range map[string]struct {
		isKG, stopped, looping bool
	}{
		"unstarted": {true, false, true}, // start closed unsent: the forkWait face still loops
		"started":   {true, true, false}, // the speak loop already stopped it
		"not KG":    {false, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			stopped, ready, sent := c.stopped, make(chan bool), make(chan struct{})
			if c.looping {
				go func() { ready <- true; close(sent) }() // the anim loop's exit, once it sees stopLooping
			}
			returned := make(chan struct{})
			go func() { forkEndKGLoop(c.isKG, &stopped, ready); close(returned) }()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("forkEndKGLoop blocked")
			}
			if want := c.isKG; stopped != want {
				t.Errorf("stopLooping = %v, want %v", stopped, want)
			}
			if c.looping {
				select {
				case <-sent:
				case <-time.After(time.Second):
					t.Error("the anim loop's exit send was never received: the loop goroutine leaks")
				}
			}
		})
	}
}

// waitStartEnds fails unless start closes without a (further) send, as StreamingKGSim's `for range start` needs.
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
			t.Errorf("start sent %d more time(s), want 0", n)
		}
	case <-time.After(time.Second):
		t.Error("start never closed: StreamingKGSim's speak loop waits forever")
	}
}
