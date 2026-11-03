// Package autohold holds behavior control of every configured robot, re-assuming whenever the stream drops.
// Mode comes from AUTO_HOLD=override|reserve|off (unset = off); the web UI's Release does not affect it.
package autohold

import (
	"strings"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// Stream is the robot's BehaviorControl stream.
type Stream interface {
	Send(*vectorpb.BehaviorControlRequest) error
	Recv() (*vectorpb.BehaviorControlResponse, error)
}

// Opener returns a stream plus the func that closes it.
type Opener func(esn string) (Stream, func(), error)

// Deps are the wire-pod internals autohold needs; sdkapp's fork_autohold.go supplies them.
type Deps struct {
	Open  Opener
	Touch func(esn string) // keeps a held robot from timing out of the SDK list
	ESNs  func() []string  // configured robots
}

// Start does nothing unless mode is override or reserve.
func Start(mode string, d Deps) {
	priority, ok := parseMode(mode)
	if !ok {
		logger.Println("Auto-hold is off (AUTO_HOLD=" + mode + ")")
		return
	}
	logger.Println("Auto-hold is on: " + priority.String())
	h := newHolder(priority, 5*time.Second, time.Minute, d.Open, d.Touch)
	go func() {
		for {
			h.scan(d.ESNs())
			time.Sleep(h.interval)
		}
	}()
}

type holder struct {
	priority   vectorpb.ControlRequest_Priority
	interval   time.Duration
	maxBackoff time.Duration
	open       Opener
	touch      func(esn string)
	quit       chan struct{}

	mu      sync.Mutex
	running map[string]bool
	granted map[string]bool
}

func parseMode(mode string) (vectorpb.ControlRequest_Priority, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "override":
		return vectorpb.ControlRequest_OVERRIDE_BEHAVIORS, true
	case "reserve":
		return vectorpb.ControlRequest_RESERVE_CONTROL, true
	}
	return 0, false
}

func newHolder(priority vectorpb.ControlRequest_Priority, interval, maxBackoff time.Duration,
	open Opener, touch func(string)) *holder {
	return &holder{
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
func (h *holder) scan(esns []string) {
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
func (h *holder) holdLoop(esn string) {
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
func (h *holder) holdOnce(esn string) (wasGranted bool, err error) {
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

type holdError string

func (e holdError) Error() string { return string(e) }

const errControlLost = holdError("control lost")

func (h *holder) setGranted(esn string, v bool) {
	h.mu.Lock()
	h.granted[esn] = v
	h.mu.Unlock()
}

func (h *holder) isGranted(esn string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.granted[esn]
}
