package wirepod_ttr

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

type listenStream struct {
	pb.ChipperGrpc_StreamingIntentGraphServer
	sent []string
}

func (s *listenStream) Send(r *pb.IntentGraphResponse) error {
	if r.IntentResult != nil {
		s.sent = append(s.sent, r.IntentResult.Action)
	} else {
		s.sent = append(s.sent, "say:"+r.SpokenText)
	}
	return nil
}

// freshListening swaps in a State and an Arm whose stream-port sets got() lists ("0,8093,0").
func freshListening(t *testing.T) (got func() string) {
	old, oldArm := forkListening, forkArm
	var mu sync.Mutex
	var sets []string
	forkArm = &listen.Arm{Log: func(string) {}, Off: func(string, string) {}, Set: func(_ string, port int) error {
		mu.Lock()
		defer mu.Unlock()
		sets = append(sets, strconv.Itoa(port))
		return nil
	}}
	forkListening = listen.NewState()
	forkListening.OnOff = forkArm.Stop
	forkHearer.State = forkListening
	arm := forkArm
	t.Cleanup(func() { arm.StopAll(); forkListening, forkHearer.State, forkArm = old, old, oldArm })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(sets, ",")
	}
}

func TestForkListenShortCircuits(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	sets := freshListening(t)
	req, _, _ := quietRig(t, "10.0.0.9")
	utt := []string{"kuulamine", "shut"} // the plugin would claim both phrases
	PluginUtterances = []*[]string{&utt}
	esn := req.Device
	var said []string
	oldSay := forkConfirmSay
	forkConfirmSay = func(_, text string) error { said = append(said, text); return nil }
	t.Cleanup(func() { forkConfirmSay = oldSay })
	for _, c := range []struct {
		say, want, sets string
		on              bool
	}{
		{"Hei Vektor, aktiveeri aktiivne kuulamine.", "Aktiivne kuulamine sees.", "0,8093", true},
		{"deaktiveeri aktiivne kuulamine", "Aktiivne kuulamine väljas.", "0,8093,0", false},
	} {
		s := &listenStream{}
		req.Stream = s
		said = nil
		if !ProcessTextAll(req, c.say, nil, true) {
			t.Fatalf("%q: ProcessTextAll = false, want forkListen to claim it", c.say)
		}
		if len(s.sent) != 1 || s.sent[0] != "intent_greeting_hello" || len(said) != 1 || said[0] != c.want {
			t.Errorf("%q: sent %v said %v, want only intent_greeting_hello (ahead of the plugin) + PC voice %q", c.say, s.sent, said, c.want)
		}
		if got := forkListening.Any(forkNow()); got != c.on {
			t.Errorf("%q: listening = %v, want %v", c.say, got, c.on)
		}
		for end := time.Now().Add(5 * time.Second); sets() != c.sets && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		}
		if got := sets(); got != c.sets {
			t.Errorf("%q: stream port sets %q, want %q", c.say, got, c.sets)
		}
	}
	forkListening.Activate(esn, forkNow())
	req.Stream = &listenStream{}
	ProcessTextAll(req, "vector, shut up", nil, true)
	if forkListening.Any(forkNow()) {
		t.Error("a quiet-lock match left active listening on")
	}
}

func TestResolveWhileOn(t *testing.T) {
	lang.UseForTest(t, lang.English)
	freshListening(t)
	if err := os.WriteFile(lang.Path(), []byte(`{"lang":"en-US","claude_key":"k","ollama_url":"http://pc:11434",
		"llm":{"en-US":{"provider":"claude","model":"claude-haiku-5-5"}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	brain := func() string {
		tg, _ := forkResolve(llm.Load(), lang.English, forkKG())
		return tg.Provider + " " + tg.Model
	}
	if got := brain(); got != "claude claude-haiku-5-5" {
		t.Fatalf("off: %q, want the saved Claude profile", got)
	}
	forkListening.Activate("00e20100", forkNow())
	if got := brain(); got != "ollama "+listen.Model {
		t.Errorf("on: %q, want listen.Model", got)
	}
	forkListening.Deactivate("00e20100", "phrase")
	if got := brain(); got != "claude claude-haiku-5-5" {
		t.Errorf("off again: %q, want Claude back", got)
	}
}

func TestForkListenStampsTurn(t *testing.T) {
	freshListening(t)
	now := time.Now()
	forkListening.Activate("on", now)
	if forkListen(nil, "what time is it", "on") || forkListen(nil, "what time is it", "off") {
		t.Fatal("a plain ask must route on")
	}
	if !forkListening.Echo("on", now, now.Add(time.Second)) {
		t.Fatal("a wake-word ask while on must open an echo window")
	}
}

// TestForkStreamHeard: a stream utterance reaches forkHearer in line (a quiet phrase turns listening off before return).
func TestForkStreamHeard(t *testing.T) {
	freshListening(t)
	_, _, naps := quietRig(t, "10.0.0.9")
	forkListening.Activate("00e20100", forkNow())
	now := time.Now()
	forkMicStream.Heard(listen.Utterance{ESN: "00e20100", Text: "vector, shut up", Start: now, End: now})
	if forkListening.Any(forkNow()) {
		t.Fatal("a streamed quiet phrase left active listening on")
	}
	select {
	case <-naps:
		t.Fatal("an overheard quiet phrase napped: only the wake path locks")
	case <-time.After(time.Second):
	}
}

// TestForkBeginSwitch: a follow-up (knowledge request, no wake word) never reaches ProcessTextAll, so forkBegin
// runs the quiet and listen phrases itself — each skips the LLM and ends the chain; a plain ask routes on.
func TestForkBeginSwitch(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	freshListening(t)
	req, _, naps := quietRig(t, "10.0.0.9")
	esn := req.Device
	var said []string
	var cuts int
	oldSay, oldCut := forkConfirmSay, forkKGCut
	forkConfirmSay = func(_, text string) error { said = append(said, text); return nil }
	forkKGCut = func(_ string, hold func()) error { cuts++; hold(); return nil }
	t.Cleanup(func() { forkConfirmSay, forkKGCut = oldSay, oldCut })
	follow := func(text string) bool {
		forkChain.Reset(esn)
		forkChain.Arm(esn, forkNow())
		_, skip := forkBegin(nil, esn, true, text)
		return skip
	}
	for _, c := range []struct {
		say, want string
		on        bool
	}{
		{"aktiveeri aktiivne kuulamine.", "Aktiivne kuulamine sees.", true},
		{"aktiivne kuulamine välja", "Aktiivne kuulamine väljas.", false},
	} {
		said = nil
		if !follow(c.say) || forkChain.Begin(esn, forkNow()) {
			t.Fatalf("%q: want skip and the armed follow-up gone", c.say)
		}
		if len(said) != 1 || said[0] != c.want || forkListening.Any(forkNow()) != c.on {
			t.Errorf("%q: said %v listening %v, want %q and %v", c.say, said, forkListening.Any(forkNow()), c.want, c.on)
		}
	}
	said = nil
	if follow("aktiivne kuulamine") || forkChain.Count(esn) != 1 || len(said) != 0 {
		t.Errorf("a non-phrase follow-up: want the LLM call (no skip, chain counted), said %v", said)
	}
	if follow("Jäävait.") || cuts != 0 {
		t.Fatal("a glued quiet phrase in a follow-up must not lock (exact match, no wake word)")
	}
	forkListening.Activate(esn, forkNow())
	if !follow("ole vait") || cuts != 1 || forkListening.Any(forkNow()) || forkChain.Begin(esn, forkNow()) {
		t.Fatalf("quiet follow-up: want skip, one placeholder cut, listening off, chain ended; cuts %d", cuts)
	}
	if _, src, _ := quiet.Until(); src != "voice:ole vait" {
		t.Errorf("lock source %q, want voice:ole vait", src)
	}
	forkNaps.Wait()
	if len(naps) != 2 {
		t.Errorf("naps %d, want 2: forkQuietLock's async one + the one held under control", len(naps))
	}
	if _, skip := forkBegin(nil, esn, false, "ole vait"); skip {
		t.Error("an intent request must not run the switch (ProcessTextAll already did)")
	}
}
