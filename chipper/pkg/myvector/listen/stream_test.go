package listen

import (
	"net"
	"strings"
	"testing"
	"time"
)

func cutTimes(t0 time.Time, ms ...int) []time.Time {
	out := make([]time.Time, len(ms))
	for i, m := range ms {
		out[i] = t0.Add(time.Duration(m) * time.Millisecond)
	}
	return out
}

// TestStreamCut drives the Cutter on explicit arrival times: one 100 ms send (3200 bytes) per tick.
func TestStreamCut(t *testing.T) {
	t0 := time.UnixMilli(1_000_000)
	send := make([]byte, pcmRate/10)
	run := func(at []time.Time, end func(*Cutter)) []Cut {
		var got []Cut
		c := &Cutter{Emit: func(u Cut) { got = append(got, u) }}
		for _, tm := range at {
			c.Add(tm, send)
		}
		end(c)
		return got
	}
	gapAt := func(ms int) func(*Cutter) {
		return func(c *Cutter) { c.Gap(t0.Add(time.Duration(ms) * time.Millisecond)) }
	}

	t.Run("gap closes one utterance", func(t *testing.T) {
		got := run(cutTimes(t0, 0, 100, 200), gapAt(200+300))
		if len(got) != 1 || len(got[0].PCM) != 3*len(send) || !got[0].Start.Equal(t0) || !got[0].End.Equal(t0.Add(200*time.Millisecond)) {
			t.Fatalf("got %d cuts %+v, want one 3-send cut 0..200 ms", len(got), got)
		}
	})
	t.Run("gap under GapCut does not cut", func(t *testing.T) {
		got := run(cutTimes(t0, 0, 100, 399, 499), gapAt(499+299))
		if len(got) != 0 {
			t.Fatalf("got %d cuts, want none: 299 ms gaps stay open", len(got))
		}
	})
	t.Run("gap between sends splits", func(t *testing.T) {
		got := run(cutTimes(t0, 0, 100, 400, 500), func(c *Cutter) { c.Flush() })
		if len(got) != 2 || len(got[0].PCM) != 2*len(send) || len(got[1].PCM) != 2*len(send) || !got[1].Start.Equal(t0.Add(400*time.Millisecond)) {
			t.Fatalf("got %d cuts, want 2+2 sends split at the 300 ms gap", len(got))
		}
	})
	t.Run("MaxUtter splits continuous audio", func(t *testing.T) {
		var at []int
		for ms := 0; ms < 16000; ms += 100 { // 16 s continuous
			at = append(at, ms)
		}
		got := run(cutTimes(t0, at...), func(c *Cutter) { c.Flush() })
		max := int(MaxUtter/time.Second) * pcmRate
		if len(got) != 2 || len(got[0].PCM) != max || len(got[1].PCM) != pcmRate {
			t.Fatalf("got %d cuts, want a %d-byte cut then the 1 s rest", len(got), max)
		}
		if !got[0].End.Equal(t0.Add(14900*time.Millisecond)) || !got[1].Start.Equal(t0.Add(15000*time.Millisecond)) {
			t.Fatalf("split times %v / %v, want 14.9 s / 15 s", got[0].End.Sub(t0), got[1].Start.Sub(t0))
		}
	})
	t.Run("disconnect flushes", func(t *testing.T) {
		got := run(cutTimes(t0, 0, 100), func(c *Cutter) { c.Flush() })
		if len(got) != 1 || len(got[0].PCM) != 2*len(send) {
			t.Fatalf("got %d cuts, want the open 2-send utterance", len(got))
		}
		if got = run(nil, func(c *Cutter) { c.Flush() }); len(got) != 0 {
			t.Fatalf("empty flush emitted %d cuts", len(got))
		}
	})
}

// TestStreamServe sends over loopback TCP: a real GapCut silence cuts, the disconnect flushes, logs carry counts only.
func TestStreamServe(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	heard := make(chan Utterance, 4)
	logs := make(chan string, 16)
	s := &Stream{
		ESN:  func(ip string) string { return map[string]string{"127.0.0.1": "00e20145"}[ip] },
		Lang: func() string { return "en-US" },
		STT: func(lang string, pcm []byte) (string, error) {
			return strings.Repeat("word ", len(pcm)/3200), nil
		},
		Heard: func(u Utterance) { heard <- u },
		Log:   func(l string) { logs <- l },
	}
	go s.Serve(l)
	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	next := func(want int, why string) {
		t.Helper()
		select {
		case u := <-heard:
			if n := len(strings.Fields(u.Text)); u.ESN != "00e20145" || n != want || u.End.Before(u.Start) {
				t.Fatalf("%s: utterance %+v (%d words), want %d words", why, u, n, want)
			}
		case <-time.After(5 * GapCut):
			t.Fatalf("%s: no utterance", why)
		}
	}
	c.Write(make([]byte, 3200))
	next(1, "silence after the bytes (read deadline)") // the cut lands while the connection stays open
	c.Write(make([]byte, 6400))
	time.Sleep(GapCut / 3)
	c.Close()
	next(2, "disconnect flush")
	var heardLog int
	for len(logs) > 0 {
		if l := <-logs; strings.HasPrefix(l, "myvector listen: heard ") {
			heardLog++
			if strings.Contains(l, "word ") {
				t.Fatalf("log carries the text: %q", l)
			}
		}
	}
	if heardLog != 2 {
		t.Fatalf("%d heard logs, want 2", heardLog)
	}
}
