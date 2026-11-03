package sdkapp

// AUTO_HOLD=override|reserve|off (unset = off): hold behavior control of every configured robot,
// re-assuming whenever the stream drops. The web UI's Release does not affect it.

import (
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

type autoHolder struct {
	priority vectorpb.ControlRequest_Priority
	interval time.Duration
	open     func(esn string) (holdStream, error)
	touch    func(esn string)
	quit     chan struct{}

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
	h := newAutoHolder(priority, 5*time.Second, openRobotHoldStream, resetConnTimer)
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

func newAutoHolder(priority vectorpb.ControlRequest_Priority, interval time.Duration,
	open func(string) (holdStream, error), touch func(string)) *autoHolder {
	return &autoHolder{
		priority: priority,
		interval: interval,
		open:     open,
		touch:    touch,
		quit:     make(chan struct{}),
		running:  map[string]bool{},
		granted:  map[string]bool{},
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

func (h *autoHolder) holdLoop(esn string) {
	lastErr := ""
	for {
		err := h.holdOnce(esn)
		if err != nil && err.Error() != lastErr {
			logger.Println("Auto-hold for " + esn + " ended, retrying: " + err.Error())
			lastErr = err.Error()
		}
		select {
		case <-h.quit:
			return
		case <-time.After(h.interval):
		}
	}
}

// holdOnce requests control and blocks until the stream errors or control is lost.
func (h *autoHolder) holdOnce(esn string) error {
	defer h.setGranted(esn, false)
	s, err := h.open(esn)
	if err != nil {
		return err
	}
	err = s.Send(&vectorpb.BehaviorControlRequest{
		RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
			ControlRequest: &vectorpb.ControlRequest{Priority: h.priority},
		},
	})
	if err != nil {
		return err
	}
	for {
		resp, err := s.Recv()
		if err != nil {
			return err
		}
		switch {
		case resp.GetControlGrantedResponse() != nil:
			logger.Println("Auto-hold: control granted for " + esn)
			h.setGranted(esn, true)
		case resp.GetControlLostEvent() != nil, resp.GetReservedControlLostEvent() != nil:
			return errControlLost
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

func openRobotHoldStream(esn string) (holdStream, error) {
	robot, _, err := getRobot(esn)
	if err != nil {
		return nil, err
	}
	return robot.Vector.Conn.BehaviorControl(robot.Ctx)
}

func resetConnTimer(esn string) {
	for i := range robots {
		if strings.EqualFold(robots[i].ESN, esn) {
			robots[i].ConnTimer = 0
		}
	}
}
