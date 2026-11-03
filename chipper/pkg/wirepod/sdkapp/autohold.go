package sdkapp

// AUTO_HOLD=override|reserve|off (unset = off): hold behavior control of every configured robot,
// re-assuming whenever the stream drops. The web UI's Release does not affect it.

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

type holdStream interface {
	Send(*vectorpb.BehaviorControlRequest) error
	Recv() (*vectorpb.BehaviorControlResponse, error)
}

// holdOpener returns a stream plus the func that closes it.
type holdOpener func(esn string) (holdStream, func(), error)

type autoHolder struct {
	priority   vectorpb.ControlRequest_Priority
	interval   time.Duration
	maxBackoff time.Duration
	open       holdOpener
	touch      func(esn string)
	quit       chan struct{}

	mu      sync.Mutex
	running map[string]bool
	granted map[string]bool
}

func autoHoldPriority(mode string) (vectorpb.ControlRequest_Priority, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "override":
		return vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, true
	case "reserve":
		return vectorpb.ControlRequest_RESERVE_CONTROL, true
	}
	return 0, false
}

func InitAutoHold() {
	mode := os.Getenv("AUTO_HOLD")
	priority, ok := autoHoldPriority(mode)
	if !ok {
		logger.Println("Auto-hold is off (AUTO_HOLD=" + mode + ")")
		return
	}
	logger.Println("Auto-hold is on: " + priority.String())
	h := newAutoHolder(priority, 5*time.Second, time.Minute, openRobotHoldStream, resetConnTimer)
	go func() {
		for {
			var esns []string
			for _, bot := range vars.BotInfo.Robots {
				esns = append(esns, bot.Esn)
			}
			h.scan(esns)
			time.Sleep(h.interval)
		}
	}()
}

func newAutoHolder(priority vectorpb.ControlRequest_Priority, interval, maxBackoff time.Duration,
	open holdOpener, touch func(string)) *autoHolder {
	return &autoHolder{
		priority:   priority,
		interval:   interval,
		maxBackoff: maxBackoff,
		open:       open,
		touch:      touch,
		quit:       make(chan struct{}),
		running:    map[string]bool{},
		granted:    map[string]bool{},
	}
}

// scan starts a hold loop for each new robot and keeps held robots from timing out of the SDK list.
func (h *autoHolder) scan(esns []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, esn := range esns {
		esn = strings.ToLower(strings.TrimSpace(esn))
		if esn == "" {
			continue
		}
		if !h.running[esn] {
			h.running[esn] = true
			go h.holdLoop(esn)
		}
		if h.granted[esn] {
			h.touch(esn)
		}
	}
}

// holdLoop retries every interval after a held stream ends; attempts that never get control back off to maxBackoff.
func (h *autoHolder) holdLoop(esn string) {
	lastErr := ""
	wait := h.interval
	for {
		wasGranted, err := h.holdOnce(esn)
		if wasGranted {
			lastErr = ""
			wait = h.interval
		}
		if err != nil && err.Error() != lastErr {
			logger.Println("Auto-hold for " + esn + " ended, retrying: " + err.Error())
			lastErr = err.Error()
		}
		select {
		case <-h.quit:
			return
		case <-time.After(wait):
		}
		if !wasGranted && wait < h.maxBackoff {
			wait *= 2
			if wait > h.maxBackoff {
				wait = h.maxBackoff
			}
		}
	}
}

// holdOnce requests control and blocks until the stream errors or control is lost.
func (h *autoHolder) holdOnce(esn string) (wasGranted bool, err error) {
	defer h.setGranted(esn, false)
	s, closeStream, err := h.open(esn)
	if err != nil {
		return false, err
	}
	defer closeStream()
	err = s.Send(&vectorpb.BehaviorControlRequest{
		RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
			ControlRequest: &vectorpb.ControlRequest{Priority: h.priority},
		},
	})
	if err != nil {
		return false, err
	}
	for {
		resp, err := s.Recv()
		if err != nil {
			return wasGranted, err
		}
		switch {
		case resp.GetControlGrantedResponse() != nil:
			logger.Println("Auto-hold: control granted for " + esn)
			wasGranted = true
			h.setGranted(esn, true)
		case resp.GetControlLostEvent() != nil, resp.GetReservedControlLostEvent() != nil:
			return wasGranted, errControlLost
		}
	}
}

type autoHoldError string

func (e autoHoldError) Error() string { return string(e) }

const errControlLost = autoHoldError("control lost")

func (h *autoHolder) setGranted(esn string, v bool) {
	h.mu.Lock()
	h.granted[esn] = v
	h.mu.Unlock()
}

func (h *autoHolder) isGranted(esn string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.granted[esn]
}

// openRobotHoldStream probes :443 first so an offline robot never reaches newRobot, which leaks a conn per failure.
func openRobotHoldStream(esn string) (holdStream, func(), error) {
	for _, bot := range vars.BotInfo.Robots {
		if strings.EqualFold(bot.Esn, esn) && bot.IPAddress != "" {
			c, err := net.DialTimeout("tcp", bot.IPAddress+":443", 3*time.Second)
			if err != nil {
				return nil, nil, err
			}
			c.Close()
		}
	}
	robot, _, err := getRobot(esn)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(robot.Ctx)
	s, err := robot.Vector.Conn.BehaviorControl(ctx)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return s, cancel, nil
}

func resetConnTimer(esn string) {
	rs := robots // removeRobot may swap in a shorter slice mid-loop
	for i := range rs {
		if strings.EqualFold(rs[i].ESN, esn) {
			rs[i].ConnTimer = 0
		}
	}
}
