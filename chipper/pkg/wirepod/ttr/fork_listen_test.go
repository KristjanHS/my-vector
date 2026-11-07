package wirepod_ttr

import (
	"os"
	"testing"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
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

func freshListening(t *testing.T) {
	old := forkListening
	forkListening = listen.NewState()
	t.Cleanup(func() { forkListening = old })
}

func TestForkListenShortCircuits(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	freshListening(t)
	req, _, _ := quietRig(t, "10.0.0.9")
	utt := []string{"kuulamine", "shut"} // the plugin would claim both phrases
	PluginUtterances = []*[]string{&utt}
	esn := req.Device
	for _, c := range []struct {
		say, want string
		on        bool
	}{
		{"Hei Vektor, aktiveeri aktiivne kuulamine.", "say:Aktiivne kuulamine sees.", true},
		{"deaktiveeri aktiivne kuulamine", "say:Aktiivne kuulamine väljas.", false},
	} {
		s := &listenStream{}
		req.Stream = s
		if !ProcessTextAll(req, c.say, nil, true) {
			t.Fatalf("%q: ProcessTextAll = false, want forkListen to claim it", c.say)
		}
		if len(s.sent) != 1 || s.sent[0] != c.want {
			t.Errorf("%q: sent %v, want only %q (ahead of the plugin)", c.say, s.sent, c.want)
		}
		if got := forkListening.Any(forkNow()); got != c.on {
			t.Errorf("%q: listening = %v, want %v", c.say, got, c.on)
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
