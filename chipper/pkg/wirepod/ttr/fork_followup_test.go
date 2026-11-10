package wirepod_ttr

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/followup"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
)

// decideServer answers each chat completion with reply after delay (status 500 when code is set), recording the body.
func decideServer(t *testing.T, reply string, code int, delay time.Duration, body *map[string]any) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, body)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		if code != 0 {
			w.WriteHeader(code)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": reply}}}})
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func TestForkDecide(t *testing.T) {
	lang.UseForTest(t, lang.English)
	old := forkDecideTimeout
	forkDecideTimeout = 200 * time.Millisecond
	t.Cleanup(func() { forkDecideTimeout = old })
	for _, provider := range []string{llm.Ollama, llm.Claude} {
		for _, c := range []struct {
			name, reply string
			code        int
			delay       time.Duration
			want        bool
		}{
			{"YES", "YES", 0, 0, true},
			{"no", "no", 0, 0, false},
			{"500", "", 500, 0, false},
			{"slow YES", "YES", 0, time.Second, false},
		} {
			var body map[string]any
			tg := llm.Target{Provider: provider, BaseURL: decideServer(t, c.reply, c.code, c.delay, &body), Key: "k", Model: "qwen3.5:4b"}
			select {
			case got := <-forkDecide(tg, "ask me a riddle", "What has keys?"):
				if got != c.want {
					t.Errorf("%s %s: %v, want %v", provider, c.name, got, c.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s %s: forkDecide never answered", provider, c.name)
			}
			if c.name != "YES" {
				continue
			}
			if provider == llm.Claude {
				th, _ := body["thinking"].(map[string]any)
				if body["model"] != followup.ClaudeModel || th["type"] != "disabled" || body["max_completion_tokens"] != float64(followup.MaxTokens) {
					t.Errorf("claude body %v: want %s, thinking disabled, max_completion_tokens %d", body, followup.ClaudeModel, followup.MaxTokens)
				}
			} else if body["model"] != "qwen3.5:4b" || body["reasoning_effort"] != "none" {
				t.Errorf("ollama body %v: want the resolved model, reasoning_effort none", body)
			}
		}
	}
}

func roles(ms []openai.ChatCompletionMessage) string {
	var rs []string
	for _, m := range ms {
		rs = append(rs, m.Role)
	}
	return strings.Join(rs, ",")
}

// TestForkAIReqHistory: F3 — the remembered chat follows the system message with or without SaveChat, never twice.
func TestForkAIReqHistory(t *testing.T) {
	lang.UseForTest(t, lang.English)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	old := vars.RememberedChats
	t.Cleanup(func() { vars.RememberedChats = old })
	for _, save := range []bool{false, true} {
		vars.RememberedChats = nil
		vars.APIConfig.Knowledge.SaveChat = save
		if got := roles(CreateAIReq("hi", "esn", false, true).Messages); got != "system,user" {
			t.Fatalf("save_chat %v, empty history: %s", save, got)
		}
		forkAfterReply(make(forkTurn, 1), "esn", true, "ask me a riddle", "What has keys?")
		if !save && len(GetChat("esn").Chats) != 2 {
			t.Fatalf("save_chat false: forkAfterReply remembered %d messages, want 2", len(GetChat("esn").Chats))
		}
		if save && len(GetChat("esn").Chats) != 0 {
			t.Fatal("save_chat true: forkAfterReply remembered on top of upstream's Remember")
		}
		vars.RememberedChats = nil
		Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "ask me a riddle"},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "What has keys?"}, "esn")
		ms := CreateAIReq("a piano", "esn", false, true).Messages
		if got := roles(ms); got != "system,user,assistant,user" || ms[1].Content != "ask me a riddle" || ms[3].Content != "a piano" {
			t.Errorf("save_chat %v: %s %q, want system,user,assistant,user with the ask last", save, got, ms[len(ms)-1].Content)
		}
	}
}

// TestForkAfterReplyDecider: F1 — the decider starts on the resolved brain's target; no fork target hands over nil.
func TestForkAfterReplyDecider(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	vars.APIConfig.Knowledge.SaveChat = true
	oldDecider := forkDecider
	t.Cleanup(func() { forkDecider = oldDecider })
	var got []llm.Target
	forkDecider = func(tg llm.Target, _, _ string) <-chan bool { got = append(got, tg); return make(chan bool) }
	for _, c := range []struct {
		l    string
		want bool
	}{{lang.Estonian, true}, {lang.English, false}} {
		lang.UseForTest(t, c.l)
		got = nil
		turn := make(forkTurn, 1)
		forkAfterReply(turn, "fu-decider", true, "ask me a riddle", "What has keys?")
		d := <-turn
		if (d != nil) != c.want || (len(got) == 1) != c.want || (c.want && got[0].BaseURL != ollamaEndpoint) {
			t.Errorf("%s: decider %v, targets %v; want started=%v on %s", c.l, d != nil, got, c.want, ollamaEndpoint)
		}
	}
}

// decided is a request's forkTurn holding a decider channel with v (nil v = the decider never answers).
func decided(v *bool) forkTurn {
	ch := make(chan bool, 1)
	if v != nil {
		ch <- *v
	}
	turn := make(forkTurn, 1)
	turn <- ch
	return turn
}

// TestForkFollowUp: F1/F2 — this request's YES arms the chain and fires, even when its decider is handed over
// late; NO, timeout, skip, no decider, nothing handed over and the cap do not.
func TestForkFollowUp(t *testing.T) {
	oldFire, oldWait := forkFire, forkFollowUpWait
	t.Cleanup(func() { forkFire, forkFollowUpWait = oldFire, oldWait })
	forkFollowUpWait = 100 * time.Millisecond
	fired := 0
	forkFire = func(*vector.Vector) { fired++ }
	yes, no := true, false
	late := make(forkTurn, 1)
	go func() { time.Sleep(20 * time.Millisecond); late <- <-decided(&yes) }()
	nilTurn := make(forkTurn, 1)
	nilTurn <- nil
	for _, c := range []struct {
		name string
		turn forkTurn
		skip bool
		want bool
	}{
		{"YES", decided(&yes), false, true},
		{"late YES", late, false, true},
		{"NO", decided(&no), false, false},
		{"timeout", decided(nil), false, false},
		{"skip", decided(&yes), true, false},
		{"no decider", nilTurn, false, false},
		{"nothing handed over", make(forkTurn, 1), false, false},
	} {
		esn := "fu-" + c.name
		forkChain.Reset(esn)
		fired = 0
		got := forkFollowUp(nil, c.turn, esn, c.skip)
		if got != c.want || fired != map[bool]int{true: 1}[c.want] {
			t.Errorf("%s: fired %v (%d), want %v", c.name, got, fired, c.want)
		}
		if armed := forkChain.Begin(esn, forkNow()); armed != c.want {
			t.Errorf("%s: chain armed %v, want %v", c.name, armed, c.want)
		}
	}
	robot := &vector.Vector{}
	_, istop, end := forkInterruptBot(robot, make(chan bool, 1))
	istop <- true
	for forkSpeakCtx(robot).Err() == nil {
		time.Sleep(time.Millisecond)
	}
	forkChain.Reset("fu-cut")
	fired = 0
	if forkFollowUp(robot, decided(&yes), "fu-cut", false) || fired != 0 {
		t.Errorf("follow-up fired after an interrupt cut the speech")
	}
	end()
	esn := "fu-cap"
	forkChain.Reset(esn)
	for i := 0; i < followup.Max; i++ {
		if !forkFollowUp(nil, decided(&yes), esn, false) || !forkChain.Begin(esn, forkNow()) {
			t.Fatalf("follow-up %d did not fire", i+1)
		}
	}
	if forkFollowUp(nil, decided(&yes), esn, false) {
		t.Errorf("follow-up %d fired over the cap of %d", followup.Max+1, followup.Max)
	}
	// G1: active listening caps the chain at MaxListening; without it the same count still fires.
	for _, active := range []bool{true, false} {
		esn := "fu-cap-listening-" + map[bool]string{true: "on", false: "off"}[active]
		forkChain.Reset(esn)
		if active {
			forkListening.Activate(esn, forkNow())
			t.Cleanup(func() { forkListening.Deactivate(esn, "test") })
		}
		for i := 0; i < followup.MaxListening; i++ {
			if !forkFollowUp(nil, decided(&yes), esn, false) || !forkChain.Begin(esn, forkNow()) {
				t.Fatalf("active %v: follow-up %d did not fire", active, i+1)
			}
		}
		if got := forkFollowUp(nil, decided(&yes), esn, false); got == active {
			t.Errorf("active %v: follow-up %d fired %v", active, followup.MaxListening+1, got)
		}
	}
}

// TestForkBegin: F2/B1 — an armed knowledge request counts, any other request resets, a blank one resets and skips;
// every knowledge skip cuts streamingKG's "bla bla" placeholder, every intent-graph skip sends one noaudio intent.
func TestForkBegin(t *testing.T) {
	esn := "fu-begin"
	oldCut, oldNoAudio := forkKGCut, forkNoAudio
	t.Cleanup(func() { forkKGCut, forkNoAudio = oldCut, oldNoAudio })
	cuts, noAudio := 0, 0
	forkKGCut = func(_ string, hold func()) error { cuts++; hold(); return nil }
	forkNoAudio = func(interface{}) { noAudio++ }
	begin := func(isKG bool, text string) bool {
		cutsBefore, noAudioBefore := cuts, noAudio
		turn, skip := forkBegin(nil, esn, isKG, text)
		if turn == nil || cap(turn) != 1 {
			t.Fatalf("forkBegin(%v, %q): turn %v, want a buffered forkTurn", isKG, text, turn)
		}
		wantCuts, wantNoAudio := 0, 0
		if skip && isKG {
			wantCuts = 1
		} else if skip {
			wantNoAudio = 1
		}
		if cuts-cutsBefore != wantCuts || noAudio-noAudioBefore != wantNoAudio {
			t.Errorf("forkBegin(%v, %q): skip %v, %d cuts, %d noaudio, want %d, %d", isKG, text, skip,
				cuts-cutsBefore, noAudio-noAudioBefore, wantCuts, wantNoAudio)
		}
		return skip
	}
	arm := func() {
		forkChain.Reset(esn)
		forkChain.Arm(esn, forkNow())
		if begin(true, "a piano") || forkChain.Count(esn) != 1 {
			t.Fatalf("armed knowledge request: count %d, want 1", forkChain.Count(esn))
		}
		forkChain.Arm(esn, forkNow())
	}
	arm()
	if begin(false, "dance") || forkChain.Count(esn) != 0 || forkChain.Begin(esn, forkNow()) {
		t.Errorf("intent request: count %d, want the chain reset", forkChain.Count(esn))
	}
	if begin(false, "") {
		t.Error("blank intent request: skipped, want it left to the intent path (!isKG wins)")
	}
	arm()
	if !begin(true, "  ") || forkChain.Count(esn) != 0 || forkChain.Begin(esn, forkNow()) {
		t.Errorf("blank follow-up: want skip and the chain reset, count %d", forkChain.Count(esn))
	}
	arm()
	followup.SetNoSpeech(esn, 0.94)
	if !begin(true, "tere.") || forkChain.Count(esn) != 0 || forkChain.Begin(esn, forkNow()) {
		t.Errorf("no-speech follow-up: want skip and the chain reset, count %d", forkChain.Count(esn))
	}
	arm()
	followup.SetNoSpeech(esn, 0.015)
	if begin(true, "mis on oecd?") || forkChain.Count(esn) != 2 {
		t.Errorf("spoken follow-up: want it answered, count %d", forkChain.Count(esn))
	}
	arm()
	followup.SetNoSpeech(esn, 0.7)
	if !begin(true, "tere.") {
		t.Error("armed follow-up at 0.7: want skip (NoSpeech, not WakeNoSpeech)")
	}
	forkChain.Reset(esn)
	followup.SetNoSpeech(esn, 0.94)
	if !begin(true, "tere.") || forkChain.Count(esn) != 0 {
		t.Error("unarmed knowledge request (\"I have a question\") at 0.94: want skip")
	}
	followup.SetNoSpeech(esn, 0.7)
	if begin(true, "mis on oecd?") {
		t.Error("unarmed knowledge request at 0.7: want it answered")
	}
	arm()
	followup.SetNoSpeech(esn, 0.9)
	if !begin(false, "aitäh.") || forkChain.Count(esn) != 0 {
		t.Error("silent wake (intent-graph) at 0.9: want skip, noaudio and the chain reset")
	}
	followup.SetNoSpeech(esn, 0.7)
	if begin(false, "teine, palun.") {
		t.Error("intent-graph wake at 0.7: want it answered")
	}
}

// TestForkRepeats: G2 — a remembered question is not spoken and leaves full as the kept text; a reply of only
// dropped repeats ends the chain with no decider.
func TestForkRepeats(t *testing.T) {
	old := vars.RememberedChats
	t.Cleanup(func() { vars.RememberedChats = old })
	vars.RememberedChats = nil
	esn := "fu-repeats"
	q := "Kas sinuk on mingid eriti huvilised tegurid?"
	Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "tere"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "Tere! " + q}, esn)
	r := forkRepeatFilter(esn)
	full := "Hea küll. " + q
	if !r.drop(q, "", []string{"Hea küll."}, &full) || full != "Hea küll." {
		t.Errorf("repeat: dropped %d, full %q, want 1 and %q", r.dropped, full, "Hea küll.")
	}
	full = "Hea küll. " + q + " See on hea."
	if !r.drop(q, " See on hea.", []string{"Hea küll."}, &full) || full != "Hea küll. See on hea." {
		t.Errorf("mid-reply repeat: full %q, want %q (the kept chunks joined)", full, "Hea küll. See on hea.")
	}
	if r.drop("Mis kell praegu on?", "", nil, &full) {
		t.Error("a new question dropped")
	}
	if r.only(make(forkTurn, 1), []string{"Hea küll."}) {
		t.Error("only with a kept chunk")
	}
	forkChain.Arm(esn, forkNow())
	forkChain.Begin(esn, forkNow())
	turn := make(forkTurn, 1)
	if !r.only(turn, nil) || forkChain.Count(esn) != 0 || len(turn) != 1 || <-turn != nil {
		t.Errorf("all dropped: chain count %d, turn len %d, want reset and a nil decider", forkChain.Count(esn), len(turn))
	}
	if forkRepeatFilter(esn).only(make(forkTurn, 1), nil) {
		t.Error("only with nothing dropped")
	}
}

// TestForkNextChunk: C1 — the speak loop re-checks after every wake: a wake with no new chunk neither indexes past
// the slice nor ends the loop, and a reply whose last sentence was a dropped repeat ends the loop (rep.wake) instead
// of leaving it waiting for a sentence that never comes.
func TestForkNextChunk(t *testing.T) {
	var mu sync.Mutex
	var chunks []string
	var done bool
	checked := make(chan struct{}, 16) // one per done() check: the loop is about to wait
	get := func() []string { mu.Lock(); defer mu.Unlock(); return chunks }
	isDone := func() bool {
		mu.Lock()
		defer mu.Unlock()
		select {
		case checked <- struct{}{}:
		default:
		}
		return done
	}
	ready := make(chan string)
	next := func(n int) <-chan string {
		ch := make(chan string, 1)
		go func() {
			s, ok := forkNextChunk(get, isDone, n, ready)
			if !ok {
				s = "<end>"
			}
			ch <- s
		}()
		return ch
	}
	want := func(ch <-chan string, want, what string) {
		t.Helper()
		select {
		case got := <-ch:
			if got != want {
				t.Errorf("%s: got %q, want %q", what, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: speak loop still waiting", what)
		}
	}
	got := next(0)
	ready <- "" // received: the loop was waiting
	select {
	case s := <-got:
		t.Fatalf("a wake with no chunk returned %q", s)
	case <-time.After(50 * time.Millisecond):
	}
	mu.Lock()
	chunks = []string{"Hea küll."}
	mu.Unlock()
	ready <- "Hea küll."
	want(got, "Hea küll.", "kept sentence")

	for len(checked) > 0 {
		<-checked
	}
	got = next(1)
	<-checked // spoke every chunk, not done: waits for the dropped question that never comes
	mu.Lock()
	done = true // stream EOF
	mu.Unlock()
	(&forkRepeats{dropped: 1}).wake(ready)
	want(got, "<end>", "last sentence a dropped repeat")

	(&forkRepeats{}).wake(ready)
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Error("no wake with nothing dropped: a lost send or an extraBit append leaves the loop waiting")
	}
}

func TestForkDecideByRule(t *testing.T) {
	for reply, want := range map[string]bool{"What has keys?": true, "It's half past seven.": false} {
		var body map[string]any
		tg := llm.Target{Provider: llm.Ollama, BaseURL: decideServer(t, "NO", 0, 0, &body), Key: "k", Model: "apertus-estllm:8b"}
		if got := <-forkDecide(tg, "ask", reply); got != want {
			t.Errorf("apertus %q: %v, want %v", reply, got, want)
		}
		if body != nil {
			t.Errorf("apertus %q: the decider called the LLM", reply)
		}
	}
}
