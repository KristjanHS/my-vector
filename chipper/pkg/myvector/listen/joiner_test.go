package listen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const clipBytes = 3 * 32000

// joinRig fakes myvector-stt: clip n is pcm bytes of value n; full[n] answers tail+clip, last[n] its last 2 s.
type joinRig struct {
	j          *Joiner
	full, last map[byte]string
	got        []Utterance
	t0         time.Time
}

func newJoinRig() *joinRig {
	r := &joinRig{full: map[byte]string{}, last: map[byte]string{}, t0: time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)}
	r.j = &Joiner{
		STT: func(pcm []byte) (string, error) {
			n := pcm[len(pcm)-1]
			if len(pcm) == tailBytes {
				return r.last[n], nil
			}
			return r.full[n], nil
		},
		Heard: func(u Utterance) { r.got = append(r.got, u) },
	}
	return r
}

func (r *joinRig) clip(n byte) {
	if err := r.j.Clip(esn, r.t0.Add(time.Duration(n-1)*4*time.Second), []byte(strings.Repeat(string(rune(n)), clipBytes))); err != nil {
		panic(err)
	}
}

func TestJoinerCut(t *testing.T) {
	r := newJoinRig()
	r.full[1], r.last[1] = "is the capital of", "capital of"
	r.full[2], r.last[2] = "Capital of Estonia Riga?", ""
	r.full[3] = ""
	r.clip(1)
	if len(r.got) != 0 {
		t.Fatalf("speech at the cut closed it: %+v", r.got)
	}
	r.clip(2)
	r.clip(3)
	want := Utterance{ESN: esn, Text: "is the capital of Estonia Riga?", Start: r.t0, End: r.t0.Add(7 * time.Second)}
	if len(r.got) != 1 || r.got[0] != want {
		t.Fatalf("got %+v, want one %+v", r.got, want)
	}
}

func TestJoinerCap(t *testing.T) {
	r := newJoinRig()
	for n, w := range []string{"", "news at", "nine the", "weather"} {
		r.full[byte(n)], r.last[byte(n)] = w, w
	}
	r.clip(1)
	r.clip(2)
	if len(r.got) != 0 {
		t.Fatalf("closed before MaxClips: %+v", r.got)
	}
	r.clip(3)
	if len(r.got) != 1 || r.got[0].Text != "news at nine the weather" {
		t.Fatalf("at MaxClips: %+v", r.got)
	}
}

func TestMergeOverlap(t *testing.T) {
	for _, c := range []struct {
		prev, cur string
		want      int
	}{
		{"is the capital of", "Capital, of Estonia", 2},
		{"a b c", "d e", 0},
		{"", "a b", 0},
		{"a b", "a b", 2},
	} {
		if got := overlap(strings.Fields(c.prev), strings.Fields(c.cur)); got != c.want {
			t.Errorf("overlap(%q, %q) = %d, want %d", c.prev, c.cur, got, c.want)
		}
	}
}

func TestClipHandler(t *testing.T) {
	s, r := NewState(), newJoinRig()
	r.full[1] = "hello there"
	h := ClipHandler(s, r.j, func(string) {})
	post := func(remote string) int {
		q := httptest.NewRequest(http.MethodPost, ClipAPI+"?esn="+esn+"&start=1762516800000", strings.NewReader(strings.Repeat("\x01", clipBytes)))
		q.RemoteAddr = remote
		w := httptest.NewRecorder()
		h(w, q)
		return w.Code
	}
	if c := post("127.0.0.1:5000"); c != http.StatusGone {
		t.Fatalf("off: %d, want 410", c)
	}
	s.Activate(esn, time.Now())
	if c := post("192.168.1.20:5000"); c != http.StatusForbidden {
		t.Fatalf("LAN: %d, want 403", c)
	}
	if c := post("127.0.0.1:5000"); c != http.StatusNoContent || len(r.got) != 1 || r.got[0].Text != "hello there" {
		t.Fatalf("on: %d got %+v", c, r.got)
	}
}
