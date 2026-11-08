package listen

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// M6 arming: the robot dials StreamAddr while its StreamPortVar (patch 0016, T1) is StreamPort; 0 = off.
const (
	StreamPortVar = "MyVector_MicStreamPort"
	StreamPort    = 8093
)

// DialWait: armed with no connection this long = a miss (the robot connects within ~1 s of the var, redials 1 s after a drop).
// PollActive spaces the idle/night check while armed: G1 sends nothing in silence, so no Heard runs it.
const (
	DialWait   = 10 * time.Second
	PollActive = time.Minute
)

// ConsoleURL is vic-anim's console on the robot (:8889, as sdkapp trigger_wake_word); tests point it at httptest.
var ConsoleURL = func(ip string) string { return "http://" + ip + ":8889" }

// SetPort sets ip's StreamPortVar to port.
func SetPort(ip string, port int) error { return setPort(ConsoleURL(ip), port) }

func setPort(console string, port int) error {
	c := http.Client{Timeout: 5 * time.Second}
	r, err := c.Get(console + "/consolevarset?key=" + StreamPortVar + "&value=" + strconv.Itoa(port))
	if err != nil {
		return err
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("listen: %s: vic-anim %s", StreamPortVar, r.Status)
	}
	return nil
}

// Arm keeps each listening robot's mic stream dialled in (M6); RestartWait spaces re-arms after a miss, one more miss = Off.
type Arm struct {
	Set    func(esn string, port int) error // the robot's StreamPortVar
	Close  func(esn string)                 // Stream.Close
	Off    func(esn, why string)            // listening off after the stream gave up ("capture")
	Active func(esn string) bool            // State.Active; false ends arming via State.OnOff; nil = on
	Log    func(string)
	Wait   []time.Duration // nil = RestartWait
	Dial   time.Duration   // 0 = DialWait
	Poll   time.Duration   // 0 = PollActive

	mu   sync.Mutex
	bots map[string]*armed
	set  sync.Mutex // one Set at a time, each sending the then-wanted port, so the last one wins
}

type armed struct {
	stop chan struct{}
	kick chan struct{} // cap 1: up changed
	up   bool
}

// Start arms esn unless armed.
func (a *Arm) Start(esn string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.bots == nil {
		a.bots = map[string]*armed{}
	}
	if _, ok := a.bots[esn]; ok {
		return
	}
	b := &armed{stop: make(chan struct{}), kick: make(chan struct{}, 1)}
	a.bots[esn] = b
	go a.loop(esn, b)
}

// Stop disarms esn: port 0, then its connection closed. Never blocks (State.OnOff runs it under the State lock).
func (a *Arm) Stop(esn string) {
	a.mu.Lock()
	b, ok := a.bots[esn]
	a.mu.Unlock()
	if ok {
		a.stop(esn, b)
	}
}

func (a *Arm) stop(esn string, b *armed) {
	a.mu.Lock()
	if a.bots[esn] != b {
		a.mu.Unlock()
		return
	}
	delete(a.bots, esn)
	close(b.stop)
	a.mu.Unlock()
	go a.apply(esn)
}

// Up is Stream.Up: an armed robot's connection resets its misses; an unarmed one is told port 0 and dropped.
func (a *Arm) Up(esn string) bool {
	if !a.mark(esn, true) {
		go a.apply(esn)
		return false
	}
	return true
}

// Down is Stream.Down: the miss clock starts.
func (a *Arm) Down(esn string) { a.mark(esn, false) }

func (a *Arm) mark(esn string, up bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.bots[esn]
	if ok {
		b.up = up
		select {
		case b.kick <- struct{}{}:
		default:
		}
	}
	return ok
}

func (a *Arm) isUp(esn string, b *armed) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bots[esn] == b && b.up
}

// apply sends esn's wanted port: StreamPort while armed, else 0 and its connection closed.
func (a *Arm) apply(esn string) {
	a.set.Lock()
	defer a.set.Unlock()
	a.mu.Lock()
	_, on := a.bots[esn]
	a.mu.Unlock()
	port := 0
	if on {
		port = StreamPort
	}
	if err := a.Set(esn, port); err != nil {
		a.Log(fmt.Sprintf("myvector listen: stream port %d: %s", port, err))
	}
	if !on && a.Close != nil {
		a.Close(esn)
	}
}

func (a *Arm) loop(esn string, b *armed) {
	wait, dial, every := a.Wait, a.Dial, a.Poll
	if wait == nil {
		wait = RestartWait
	}
	if dial == 0 {
		dial = DialWait
	}
	if every == 0 {
		every = PollActive
	}
	a.apply(esn)
	miss := time.After(dial)
	poll := time.NewTicker(every)
	defer poll.Stop()
	for fails := 0; ; {
		select {
		case <-b.stop:
			return
		case <-poll.C:
			if a.Active != nil && !a.Active(esn) {
				a.stop(esn, b) // State.off already ran OnOff = Stop; this is the no-State-entry fallback
				return
			}
			continue
		case <-b.kick:
			if a.isUp(esn, b) {
				fails, miss = 0, nil
			} else if miss == nil {
				a.Log("myvector listen: stream lost")
				miss = time.After(dial)
			}
			continue
		case <-miss:
		}
		if fails == len(wait) {
			a.Log("myvector listen: stream gave up")
			a.stop(esn, b)
			a.Off(esn, "capture")
			return
		}
		a.Log("myvector listen: no stream; re-arm in " + wait[fails].String())
		select {
		case <-b.stop:
			return
		case <-time.After(wait[fails]):
		}
		fails++
		if a.isUp(esn, b) { // connected during the wait
			fails, miss = 0, nil
			continue
		}
		a.apply(esn)
		miss = time.After(dial)
	}
}
