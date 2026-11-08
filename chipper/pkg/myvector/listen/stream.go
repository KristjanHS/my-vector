package listen

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// M5 mic stream: the robot (patch 0015, T1) dials out raw TCP to StreamAddr and sends 16 kHz s16le mono PCM only
// around speech (G1 gate); no framing, no token — the PC firewall (wire-pod-guard.nft) is the auth.
const (
	StreamAddr = ":8093"
	GapCut     = 300 * time.Millisecond // no bytes this long closes an utterance (the robot's 700 ms hangover is the pause cut)
	MaxUtter   = 15 * time.Second       // audio length that splits an utterance nobody pauses in
	pcmRate    = 32000                  // bytes/s of 16 kHz s16le mono
)

// Cut is one utterance of stream audio; Start/End are the server arrival times of its first and last bytes.
type Cut struct {
	PCM        []byte
	Start, End time.Time
}

// Cutter splits a byte stream into Cuts on explicit arrival times, so tests need no clock; Emit runs synchronously.
type Cutter struct {
	Emit       func(Cut)
	buf, odd   []byte // odd: a half sample cut off by Flush, carried into the next utterance
	start, end time.Time
}

// Add takes p arriving at now: a GapCut since the last bytes closes the open utterance first, MaxUtter of audio splits.
func (c *Cutter) Add(now time.Time, p []byte) {
	c.Gap(now)
	if len(p) == 0 {
		return
	}
	if len(c.buf) == 0 {
		c.start = now
		c.buf, c.odd = append(c.buf, c.odd...), nil
	}
	c.buf = append(c.buf, p...)
	c.end = now
	for lim := int(MaxUtter/time.Second) * pcmRate; len(c.buf) >= lim; {
		rest := append([]byte{}, c.buf[lim:]...)
		c.buf = c.buf[:lim]
		c.Flush()
		if len(rest) > 0 {
			c.buf, c.start = rest, now
		}
	}
}

// Gap closes the open utterance when no bytes arrived for GapCut by now.
func (c *Cutter) Gap(now time.Time) {
	if len(c.buf) > 0 && now.Sub(c.end) >= GapCut {
		c.Flush()
	}
}

// Flush closes the open utterance, if any (disconnect).
func (c *Cutter) Flush() {
	n := len(c.buf) &^ 1
	cut := Cut{PCM: c.buf[:n], Start: c.start, End: c.end}
	c.odd, c.buf = append([]byte{}, c.buf[n:]...), nil
	if n > 0 {
		c.Emit(cut)
	}
}

// Stream receives the robot mic stream; one connection at a time, a new one closes the old.
// One worker runs Cuts through STT in arrival order and logs "heard N words"; nil STT = STT.
type Stream struct {
	ESN   func(ip string) string // robot by source IP; "" drops the connection
	Lang  func() string
	STT   func(lang string, pcm []byte) (string, error)
	Heard func(Utterance) // nil = log only (Stage 2.1)
	Log   func(string)    // nil = none

	mu   sync.Mutex
	cur  net.Conn
	once sync.Once
	q    chan streamCut
}

// streamQueue Cuts may wait for STT; a full queue drops the new Cut.
const streamQueue = 4

type streamCut struct {
	esn string
	cut Cut
}

// ListenAndServe serves StreamAddr until the listener fails.
func (s *Stream) ListenAndServe() error {
	l, err := net.Listen("tcp", StreamAddr)
	if err != nil {
		return err
	}
	return s.Serve(l)
}

// Serve accepts connections on l until it closes.
func (s *Stream) Serve(l net.Listener) error {
	s.once.Do(func() {
		s.q = make(chan streamCut, streamQueue)
		go func() {
			for j := range s.q {
				s.utter(j.esn, j.cut)
			}
		}()
	})
	for {
		c, err := l.Accept()
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		if err != nil {
			s.logf("myvector listen: stream accept: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
		esn := ""
		if s.ESN != nil {
			esn = s.ESN(ip)
		}
		if esn == "" {
			s.logf("myvector listen: stream from unknown robot " + ip)
			c.Close()
			continue
		}
		s.mu.Lock()
		if s.cur != nil {
			s.cur.Close()
		}
		s.cur = c
		s.mu.Unlock()
		s.logf("myvector listen: stream from " + esn)
		go s.read(esn, c)
	}
}

// read feeds c into a Cutter stamped on arrival; a read deadline GapCut after the last bytes realises the gap cut.
func (s *Stream) read(esn string, c net.Conn) {
	defer func() {
		s.mu.Lock()
		if s.cur == c {
			s.cur = nil
		}
		s.mu.Unlock()
		c.Close()
	}()
	cut := &Cutter{Emit: func(u Cut) {
		select {
		case s.q <- streamCut{esn, u}:
		default:
			s.logf(fmt.Sprintf("myvector listen: stream queue full, dropped %d ms", len(u.PCM)*1000/pcmRate))
		}
	}}
	defer cut.Flush()
	buf := make([]byte, 32<<10)
	for {
		n, err := c.Read(buf)
		now := time.Now()
		if n > 0 {
			cut.Add(now, buf[:n])
			_ = c.SetReadDeadline(now.Add(GapCut))
		}
		if err == nil {
			continue
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			cut.Flush() // the deadline fired: GapCut without bytes
			_ = c.SetReadDeadline(time.Time{})
			continue
		}
		return
	}
}

// utter transcribes one Cut and logs its word count (never the text).
func (s *Stream) utter(esn string, u Cut) {
	stt, lang := s.STT, ""
	if stt == nil {
		stt = STT
	}
	if s.Lang != nil {
		lang = s.Lang()
	}
	text, err := stt(lang, u.PCM)
	if err != nil {
		s.logf("myvector listen: stream stt: " + err.Error())
		return
	}
	s.logf(fmt.Sprintf("myvector listen: heard %d words", len(strings.Fields(text))))
	if s.Heard != nil {
		s.Heard(Utterance{ESN: esn, Text: text, Start: u.Start, End: u.End})
	}
}

func (s *Stream) logf(l string) {
	if s.Log != nil {
		s.Log(l)
	}
}
