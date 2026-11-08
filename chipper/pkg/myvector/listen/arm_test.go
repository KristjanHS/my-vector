package listen

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type armRig struct {
	a    *Arm
	addr string
	mu   sync.Mutex
	sets []string // value= of each StreamPortVar set
	off  []string
	logs []string // Stream + Arm
}

// newArmRig: an Arm on a loopback Stream (127.0.0.1 = esn) and an httptest vic-anim console; waits are ms.
func newArmRig(t *testing.T) *armRig {
	g := &armRig{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/consolevarset" || r.URL.Query().Get("key") != StreamPortVar {
			t.Errorf("console request %s", r.URL)
		}
		g.mu.Lock()
		g.sets = append(g.sets, r.URL.Query().Get("value"))
		g.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	g.addr = l.Addr().String()
	logf := func(l string) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.logs = append(g.logs, l)
	}
	s := &Stream{ESN: func(string) string { return esn }, STT: func(string, []byte) (string, error) { return "", nil }, Log: logf}
	g.a = &Arm{
		Set:   func(_ string, port int) error { return setPort(srv.URL, port) },
		Close: s.Close,
		Off: func(e, why string) {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.off = append(g.off, e+" "+why)
		},
		Log:  logf,
		Wait: []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond},
		Dial: 100 * time.Millisecond,
	}
	s.Up, s.Down = g.a.Up, g.a.Down
	go s.Serve(l)
	return g
}

func (g *armRig) got() (sets, off string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return strings.Join(g.sets, ","), strings.Join(g.off, ",")
}

// logged counts log lines starting with prefix.
func (g *armRig) logged(prefix string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, l := range g.logs {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (g *armRig) up() bool {
	g.a.mu.Lock()
	b := g.a.bots[esn]
	g.a.mu.Unlock()
	return b != nil && g.a.isUp(esn, b)
}

func (g *armRig) dial(t *testing.T) net.Conn {
	c, err := net.Dial("tcp", g.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// closedByServer: a read ends in EOF/reset, not the 2 s deadline.
func closedByServer(c net.Conn) bool {
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := c.Read(make([]byte, 1))
	return err != nil && !errors.Is(err, os.ErrDeadlineExceeded)
}

func TestConsoleURL(t *testing.T) {
	if got := ConsoleURL("192.168.1.50"); got != "http://192.168.1.50:8889" {
		t.Fatalf("ConsoleURL = %q, want vic-anim :8889", got)
	}
}

func TestStreamArm(t *testing.T) {
	t.Run("on sets the port; off sets 0 and closes the connection", func(t *testing.T) {
		g := newArmRig(t)
		g.a.Start(esn)
		eventually(t, "port 8093", func() bool { s, _ := g.got(); return s == "8093" })
		c := g.dial(t)
		eventually(t, "connection up", g.up)
		g.a.Stop(esn)
		eventually(t, "port 0", func() bool { s, _ := g.got(); return s == "8093,0" })
		if !closedByServer(c) {
			t.Fatal("off left the stream connection open")
		}
		if _, off := g.got(); off != "" {
			t.Fatalf("a phrase/idle off called Off: %q", off)
		}
	})
	t.Run("lost connection re-arms per wait, then off capture", func(t *testing.T) {
		g := newArmRig(t)
		g.a.Start(esn)
		c := g.dial(t)
		eventually(t, "connection up", g.up)
		c.Close()
		eventually(t, "off capture", func() bool { _, o := g.got(); return o == esn+" capture" })
		eventually(t, "port 0 last", func() bool { s, _ := g.got(); return s == "8093,8093,8093,8093,0" })
	})
	t.Run("a connection during the misses starts them over", func(t *testing.T) {
		g := newArmRig(t)
		g.a.Start(esn)
		eventually(t, "first re-arm", func() bool { s, _ := g.got(); return s == "8093,8093" })
		g.dial(t)
		eventually(t, "connection up", g.up)
		misses := g.logged("myvector listen: no stream")
		time.Sleep(500 * time.Millisecond) // > Dial + all Wait: still up, no more re-arms
		if s, o := g.got(); s != "8093,8093" || o != "" || g.logged("myvector listen: no stream") != misses {
			t.Fatalf("sets %q off %q, misses %d→%d while connected; want no re-arm, off or miss", s, o, misses, g.logged("myvector listen: no stream"))
		}
		g.a.Stop(esn)
		eventually(t, "port 0", func() bool { s, _ := g.got(); return s == "8093,8093,0" })
	})
	t.Run("unarmed robot is told port 0 and dropped", func(t *testing.T) {
		g := newArmRig(t)
		c := g.dial(t)
		if !closedByServer(c) {
			t.Fatal("an unarmed robot's connection stayed open")
		}
		eventually(t, "port 0", func() bool { s, _ := g.got(); return s == "0" })
		if g.logged("myvector listen: stream from "+esn+" while off") != 1 {
			t.Fatal("Stream accepted an unarmed robot (Up must refuse it, not only apply's Close)")
		}
	})
	t.Run("idle/night off while silent disarms", func(t *testing.T) {
		g := newArmRig(t)
		g.a.Poll = 20 * time.Millisecond
		g.a.Active = func(string) bool { return false }
		g.a.Start(esn)
		eventually(t, "port 0", func() bool { s, _ := g.got(); return s == "8093,0" })
		g.a.mu.Lock()
		n := len(g.a.bots)
		g.a.mu.Unlock()
		if n != 0 {
			t.Fatal("still armed after Active = false")
		}
	})
}
