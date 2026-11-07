package wirepod_ttr

import (
	"reflect"
	"testing"
	"time"
)

func TestForkSplitSentence(t *testing.T) {
	for _, c := range []struct{ buf, sentence, rest string }{
		{"Kell on 10.10. Ilm", "Kell on 10.10.", " Ilm"},
		{"Pii on 3.14! Jah", "Pii on 3.14!", " Jah"},
		{"Tere. Kuidas läheb? Hästi", "Tere.", " Kuidas läheb? Hästi"}, // the rest is kept, not dropped
		{"Oota... nüüd", "Oota...", " nüüd"},
		{`Ta ütles "jah." Siis`, `Ta ütles "jah."`, " Siis"},
		{"Mis kell on?", "Mis kell on?", ""},
		{"Aasta on 2026. Nüüd", "Aasta on 2026.", " Nüüd"}, // digit then space: a sentence end
	} {
		s, r, ok := forkSplitSentence(c.buf)
		if !ok || s != c.sentence || r != c.rest {
			t.Errorf("forkSplitSentence(%q) = %q, %q, %v; want %q, %q", c.buf, s, r, ok, c.sentence, c.rest)
		}
	}
	for _, buf := range []string{"Kell on 10.", "Kell on 10.1", "Pii on 3", "  "} {
		if s, r, ok := forkSplitSentence(buf); ok {
			t.Errorf("forkSplitSentence(%q) = %q, %q, true; want held", buf, s, r)
		}
	}
}

// forkStream runs chunks through the stream loop's steps: one split per chunk, then the EOF flush.
func forkStream(chunks ...string) []string {
	var out []string
	buf := ""
	for _, c := range chunks {
		buf += c
		for s, r, ok := forkSplitSentence(buf); ok; s, r, ok = forkSplitSentence(buf) {
			out, buf = append(out, s), r
		}
	}
	out, buf, _ = forkFlushHeld(out, buf)
	out, _ = forkFlushUnpunctuated(out, buf)
	return out
}

func TestForkStreamKeepsNumbers(t *testing.T) {
	for _, c := range []struct {
		chunks []string
		want   []string
	}{
		{[]string{"Kell", " on", " 10", ".", "10", ".", " Ilus", " päev", "."}, []string{"Kell on 10.10.", "Ilus päev."}},
		{[]string{"Pii on 3", ".", "14", " ehk", " nii", ". Jah", "."}, []string{"Pii on 3.14 ehk nii.", "Jah."}},
		{[]string{"Tere", ".", " Aasta on 2026", "."}, []string{"Tere.", "Aasta on 2026."}}, // held to EOF, then flushed
		{[]string{"Kell on 10", ".", "10", "."}, []string{"Kell on 10.10."}},
		{[]string{"Tere. Kuidas läheb? Hästi! Jah.", " Ok", "."}, []string{"Tere.", "Kuidas läheb?", "Hästi!", "Jah.", "Ok."}},
	} {
		if got := forkStream(c.chunks...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("forkStream(%q) = %q, want %q", c.chunks, got, c.want)
		}
	}
}

// A speak loop that caught up waits on speakReady; the held last sentence must wake it.
func TestForkSignalHeldWakesWaitingLoop(t *testing.T) {
	ch := make(chan string)
	chunks, tail, held := forkFlushHeld([]string{"Tere!"}, " Kell on 10.10.")
	if want := []string{"Tere!", "Kell on 10.10."}; !reflect.DeepEqual(chunks, want) || tail != "" || held != "Kell on 10.10." {
		t.Fatalf("forkFlushHeld = %q, %q, %q", chunks, tail, held)
	}
	forkSignalHeld(ch, held)
	select {
	case got := <-ch:
		if got != held {
			t.Errorf("speakReady got %q, want %q", got, held)
		}
	case <-time.After(time.Second):
		t.Fatal("speak loop never woken: the held sentence would hang it")
	}
}

// No receiver (the loop is still speaking): the signal gives up instead of leaking, and nothing to flush sends nothing.
func TestForkSignalHeldGivesUp(t *testing.T) {
	old := forkHeldWait
	forkHeldWait = 10 * time.Millisecond
	t.Cleanup(func() { forkHeldWait = old })
	ch := make(chan string)
	forkSignalHeld(ch, "x")
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ch:
		t.Error("signal still pending after forkHeldWait")
	default:
	}
	if _, _, held := forkFlushHeld([]string{"Tere."}, "  "); held != "" {
		t.Errorf("forkFlushHeld with a blank tail held %q", held)
	}
}
