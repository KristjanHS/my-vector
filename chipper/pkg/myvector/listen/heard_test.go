package listen

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

type rig struct {
	h      *Hearer
	now    time.Time
	reply  string
	calls  int
	user   string // the last LLM user message
	said   []string
	locked bool
	onAsk  func() // runs inside the LLM call
}

const esn = "00e20100"

func newRig(t *testing.T) *rig {
	r := &rig{now: time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local), reply: "SKIP"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		var req openai.ChatCompletionRequest
		json.NewDecoder(q.Body).Decode(&req)
		r.calls++
		r.user = req.Messages[len(req.Messages)-1].Content
		if r.onAsk != nil {
			r.onAsk()
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": r.reply}}}})
	}))
	t.Cleanup(srv.Close)
	cfg := openai.DefaultConfig("ollama")
	cfg.BaseURL = srv.URL + "/v1"
	c := openai.NewClientWithConfig(cfg)
	r.h = &Hearer{
		State:  NewState(),
		Client: func() (*openai.Client, string, bool) { return c, Model, true },
		Say:    func(_, text string) error { r.said = append(r.said, text); return nil },
		Locked: func(time.Time) bool { return r.locked },
		Now:    func() time.Time { return r.now },
	}
	r.h.State.Activate(esn, r.now)
	return r
}

// hear overhears text in a 2 s clip ending now, after advancing the clock by d.
func (r *rig) hear(d time.Duration, text string) {
	r.now = r.now.Add(d)
	r.h.Heard(Utterance{ESN: esn, Text: text, Start: r.now.Add(-2 * time.Second), End: r.now})
}

func TestHeardSkipAndSpeak(t *testing.T) {
	r := newRig(t)
	r.hear(time.Second, "we should water the plants.")
	if r.calls != 1 || len(r.said) != 0 {
		t.Fatalf("SKIP: calls %d said %v", r.calls, r.said)
	}
	r.reply = "Once every two weeks is plenty."
	r.hear(5*time.Second, "how often does a cactus need water?")
	if r.calls != 2 || len(r.said) != 1 || r.said[0] != r.reply {
		t.Fatalf("SPEAK: calls %d said %v", r.calls, r.said)
	}
	if want := User([]string{"we should water the plants."}, "how often does a cactus need water?"); r.user != want {
		t.Fatalf("user message %q, want %q", r.user, want)
	}
}

func TestHeardBackoff(t *testing.T) {
	r := newRig(t)
	r.reply = "Hi there."
	want := func(calls, said int, why string) {
		t.Helper()
		if r.calls != calls || len(r.said) != said {
			t.Fatalf("%s: calls %d said %v", why, r.calls, r.said)
		}
	}
	r.hear(time.Second, "is the shelf level?")
	r.hear(Backoff[0]-time.Second, "the shelf looks level")
	want(1, 1, "inside the 1st wait")
	r.hear(time.Second, "what did you say?")
	want(2, 2, "after the 1st wait")
	r.hear(Backoff[1]/2, "nothing at all")
	want(2, 2, "inside the 2nd wait")
	r.hear(time.Second, "do you agree, vektor?")
	want(3, 3, "named inside a wait")
	r.hear(BackoffReset, "anyone want tea?")
	r.hear(Backoff[0], "green tea, please")
	want(5, 5, "after BackoffReset the wait restarts at Backoff[0]")
}

func TestHeardAutoOff(t *testing.T) {
	r := newRig(t)
	r.hear(IdleOff, "anyone home?")
	if r.calls != 0 || r.h.State.Active(esn, r.now) {
		t.Fatalf("IdleOff: calls %d, still on", r.calls)
	}
	r = newRig(t)
	r.now = time.Date(2026, 11, 7, NightFrom-1, 50, 0, 0, time.Local)
	r.h.State.Activate(esn, r.now)
	r.hear(15*time.Minute, "good night everyone")
	if r.calls != 0 || r.h.State.Active(esn, r.now) {
		t.Fatalf("night entry: calls %d, still on", r.calls)
	}
}

func TestHeardPhrasesFirst(t *testing.T) {
	r := newRig(t)
	r.hear(time.Second, "deactivate active listening")
	if r.calls != 0 || r.h.State.Active(esn, r.now) || len(r.said) != 1 || r.said[0] != Confirm(false, false) {
		t.Fatalf("off phrase: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.hear(time.Second, "vector, shut up")
	if r.calls != 0 || r.h.State.Active(esn, r.now) || len(r.said) != 0 {
		t.Fatalf("quiet phrase: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.hear(time.Second, "Ole vaid.") // wake-path spelling only: overheard, it is just talk
	if r.calls != 1 || !r.h.State.Active(esn, r.now) {
		t.Fatalf("glued quiet phrase: calls %d active %v", r.calls, r.h.State.Active(esn, r.now))
	}
}

func TestHeardEchoGate(t *testing.T) {
	r := newRig(t)
	r.reply = "It is eight."
	from := r.now.Add(time.Second)
	r.h.State.Spoken(esn, from, from.Add(3*time.Second)) // a wake-word LLM answer
	r.hear(5*time.Second, "it is eight.")                // clip 2..4 s after from: inside the answer
	if r.calls != 0 {
		t.Fatalf("own speech: %d LLM calls", r.calls)
	}
	r.hear(3*time.Second+EchoPad, "what time is it?") // clip clear of the answer + EchoPad
	if r.calls != 1 || len(r.said) != 1 {
		t.Fatalf("wake-word answers must not start Backoff: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.h.State.Turn(esn, r.now)
	r.hear(TurnTail, "what's the weather")
	if r.calls != 0 {
		t.Fatalf("inside a wake-word turn: %d LLM calls", r.calls)
	}
	r.hear(EchoPad+2*time.Second, "anyway, the weather")
	if r.calls != 1 {
		t.Fatalf("after the turn: %d LLM calls", r.calls)
	}
}

func TestHeardWakeTurn(t *testing.T) {
	r := newRig(t)
	r.reply = "Kell on kaheksa."
	r.onAsk = func() { r.h.State.Turn(esn, r.now.Add(-time.Second)) } // wake audio began inside the clip
	r.hear(time.Second, "kui palju kell on?")
	if r.calls != 1 || len(r.said) != 0 {
		t.Fatalf("turn during the Ask: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.reply = "Kell on kaheksa."
	r.onAsk = func() { r.h.State.Turn(esn, r.now.Add(3*time.Second)); r.now = r.now.Add(4 * time.Second) } // asked again after the clip
	r.hear(time.Second, "kui palju kell on?")
	if r.calls != 1 || len(r.said) != 0 {
		t.Fatalf("turn after the clip, during the Ask: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.reply = "Hea küll."
	r.hear(time.Second, "ei, vektor, see pole õige")
	if r.calls != 1 || len(r.said) != 1 {
		t.Fatalf("\"ei, vektor\" is talk, not the wake word: calls %d said %v", r.calls, r.said)
	}
	r = newRig(t)
	r.reply = "Kell on kaheksa."
	for _, text := range []string{"Hey Vector, kui palju kell on?", "hei vektor mis kell on", "heivektor mis kell on",
		"Pumpkin Hippo, kui palju kell on?", "pampkin hipo mis kell on", "hey pumkin hippo, what time is it?"} {
		r.hear(time.Minute, text)
	}
	if r.calls != 0 || len(r.said) != 0 {
		t.Fatalf("wake text: calls %d said %v", r.calls, r.said)
	}
	r.hear(time.Minute, "mis kell on, vektor?")
	if r.calls != 1 || len(r.said) != 1 {
		t.Fatalf("named, no wake word: calls %d said %v", r.calls, r.said)
	}
}

func TestWakeLead(t *testing.T) {
	hits := []string{"Hey Vector, mis kell on?", "heivektor mis kell on", "Pumpkin Hippo, what time is it?",
		"pumpkin hippo", "pampkin hipo mis kell on", "pumkin ippo", "pämpkin hippo kell", "pumpkin hipot",
		"hey pumpkin hippo, shut up", "ei pampkin hipo mis kell on", "  ...Pumpkin, hippo!"}
	misses := []string{"hey vectors are fun", "attack vector analysis", "pumpkins and hippos at the zoo",
		"we bought a pumpkin", "the hippo is big", "mis kell on, pumpkin hippo?", "pumpkin the hippo"}
	for _, s := range hits {
		if !wakeLead.MatchString(s) {
			t.Errorf("wakeLead missed %q", s)
		}
	}
	for _, s := range misses {
		if wakeLead.MatchString(s) {
			t.Errorf("wakeLead matched %q", s)
		}
	}
}

func TestHeardOneWord(t *testing.T) {
	r := newRig(t)
	r.reply = "Hello there!"
	r.hear(time.Second, "hello?")
	if r.calls != 0 || len(r.said) != 0 {
		t.Fatalf("1 word: calls %d said %v", r.calls, r.said)
	}
	r.hear(time.Second, "vektor?")
	if r.calls != 1 {
		t.Fatalf("1 named word: calls %d", r.calls)
	}
}

func TestHeardLockedAndLog(t *testing.T) {
	r := newRig(t)
	r.h.LogDir = t.TempDir()
	r.reply = "Mars is the fourth planet."
	r.locked = true
	r.hear(time.Second, "mars is closest to the sun")
	if r.calls != 1 || len(r.said) != 0 {
		t.Fatalf("locked: calls %d said %v", r.calls, r.said)
	}
	b, _ := os.ReadFile(filepath.Join(r.h.LogDir, r.now.Format("2006-01-02")+".txt"))
	if !strings.Contains(string(b), "[SPEAK] mars is closest to the sun") {
		t.Fatalf("listen log %q", b)
	}
}

func TestHeardOneInFlight(t *testing.T) {
	r := newRig(t)
	r.reply = "Good question."
	entered, release := make(chan struct{}), make(chan struct{})
	r.onAsk = func() { entered <- struct{}{}; <-release }
	done := make(chan struct{})
	go func() { r.hear(time.Second, "is anyone hungry?"); close(done) }()
	<-entered
	r.onAsk = nil
	r.h.Heard(Utterance{ESN: esn, Text: "what should we eat?", Start: r.now.Add(-2 * time.Second), End: r.now})
	close(release)
	<-done
	if r.calls != 1 || len(r.said) != 1 {
		t.Fatalf("a second utterance during an LLM call: calls %d said %v", r.calls, r.said)
	}
}

func TestHeardOffDuringAsk(t *testing.T) {
	r := newRig(t)
	r.reply = "Hello."
	r.onAsk = func() { r.h.State.Deactivate(esn, "phrase") }
	r.hear(time.Second, "vector, are you awake?")
	if len(r.said) != 0 {
		t.Fatalf("off while the LLM answered: said %v", r.said)
	}
}

func TestTurnKeepsIdleOff(t *testing.T) {
	s, t0 := NewState(), time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local)
	s.Activate(esn, t0)
	s.Turn(esn, t0.Add(IdleOff+time.Minute))
	if s.Active(esn, t0.Add(IdleOff+time.Minute)) {
		t.Fatal("a wake-word ask after IdleOff must not revive listening")
	}
}

// TestAskRequest pins production Ask's body for qwen3.5:4b with nothing saved: thinking off, ReplyCap, its sampling.
func TestAskRequest(t *testing.T) {
	lang.UseForTest(t, lang.English)
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		json.NewDecoder(q.Body).Decode(&sent)
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":" SKIP "}}]}`))
	}))
	t.Cleanup(srv.Close)
	cfg := openai.DefaultConfig("ollama")
	cfg.BaseURL = srv.URL + "/v1"
	got, err := Ask(context.Background(), openai.NewClientWithConfig(cfg), "qwen3.5:4b", "hi")
	if err != nil || got != "SKIP" {
		t.Fatalf("Ask = %q, %v", got, err)
	}
	if sent["model"] != "qwen3.5:4b" || sent["reasoning_effort"] != "none" || sent["max_tokens"] != float64(ReplyCap) {
		t.Errorf("sent model %v, reasoning_effort %v, max_tokens %v", sent["model"], sent["reasoning_effort"], sent["max_tokens"])
	}
	s := llm.SamplingFor(llm.Ollama, "qwen3.5:4b")
	for k, v := range map[string]float32{"temperature": s.Temperature, "top_p": s.TopP, "presence_penalty": s.PresencePenalty} {
		if f, _ := sent[k].(float64); float32(f) != v {
			t.Errorf("%s = %v; want %v", k, sent[k], v)
		}
	}
	if v, ok := sent["max_completion_tokens"]; ok {
		t.Errorf("max_completion_tokens = %v; want absent", v)
	}
}

// TestHearInOrder: Hear keeps the transcript in call order and leaves the LLM call to the reply it returns.
func TestHearInOrder(t *testing.T) {
	r := newRig(t)
	u := func(text string) Utterance {
		r.now = r.now.Add(time.Second)
		return Utterance{ESN: esn, Text: text, Start: r.now.Add(-time.Second), End: r.now}
	}
	first := r.h.Hear(u("is anyone hungry?"))
	if first == nil || r.calls != 0 {
		t.Fatalf("Hear: reply %v, %d LLM calls; want a reply not yet run", first != nil, r.calls)
	}
	first()
	second := r.h.Hear(u("what should we eat?"))
	if second == nil {
		t.Fatal("second Hear: no reply after the first released its slot")
	}
	second()
	if want := "Earlier:\nis anyone hungry?\nNewest line:\nwhat should we eat?"; r.user != want {
		t.Fatalf("LLM user %q, want %q", r.user, want)
	}
}

// TestHearEchoInSay: a cut of Vector's reply reaching Hear before Say returns (no window yet) is echo, not transcript.
func TestHearEchoInSay(t *testing.T) {
	r := newRig(t)
	r.reply = "Ferns like damp soil."
	r.h.Say = func(_, text string) error {
		r.said = append(r.said, text)
		r.now = r.now.Add(time.Second)
		if f := r.h.Hear(Utterance{ESN: esn, Text: "ferns like", Start: r.now.Add(-time.Second), End: r.now}); f != nil {
			t.Error("a fragment of the reply got an LLM call")
		}
		r.now = r.now.Add(2 * time.Second)
		return nil
	}
	r.hear(time.Second, "is the fern dry?")
	r.hear(Backoff[0]+EchoPad, "should I water it?")
	if want := User([]string{"is the fern dry?"}, "should I water it?"); r.user != want {
		t.Fatalf("LLM user %q, want %q (the reply's own fragment entered the transcript)", r.user, want)
	}
}
