package wirepod_ttr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/photo"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
	"google.golang.org/grpc"
)

func TestForkPhotoAsk(t *testing.T) {
	type row struct {
		l, text string
		hit     bool
	}
	var rows []row
	for l, p := range forkPhotoPhrases {
		for _, ph := range append(append([]string{}, p.anywhere...), p.start...) {
			rows = append(rows, row{l, ph, true}, row{l, strings.ToUpper(ph[:1]) + ph[1:] + "?", true}, row{l, strings.ToUpper(ph) + "!", true})
		}
	}
	rows = append(rows,
		row{lang.Estonian, "vaatame", false},
		row{lang.English, "looking good", false},
		row{lang.Estonian, "no vaata, mul on küsimus", false},
		row{lang.English, "can you look at the weather", false},
		row{lang.Estonian, "Vaata, mis kell on?", true},
		row{lang.Estonian, "Vector, vaata siia", true},
		row{lang.English, "hey vector, look at this", true},
		row{lang.English, "Vector, what do you see?", true},
		row{lang.Estonian, "Ütle, mida sa näed praegu.", true},
		row{lang.English, "mida sa näed", false},
		row{lang.Estonian, "Mida sa na\u0308ed?", true}, // NFD: a + combining diaeresis
		row{lang.Estonian, "tee\u0331 pilt", false},     // e + macron below has no composed form: the mark stays in its word
		row{lang.Estonian, "Vektor, vaata siia", true},
		row{lang.Estonian, "hei vektor, vaata siia", true},
		row{lang.Estonian, "Tee uus pilt!", true}, // F7
		row{lang.Estonian, "tee uue pildi", true},
		row{lang.Estonian, "uue pildi palun", true},
		row{lang.Estonian, "Pildista!", true},
		row{lang.Estonian, "kas sa saad pildistada?", true},
		row{lang.English, "Take another picture.", true},
		row{lang.Estonian, "kus pildil joogamatt on?", false},
		row{lang.Estonian, "mis veel pildil on?", false},
		row{lang.Estonian, "tee mulle kohvi", false},
		row{lang.English, "what is new today", false},
	)
	if len(rows) != 3*10+24 {
		t.Fatalf("%d rows, want 54 (10 phrases × 3 + 24)", len(rows))
	}
	for _, r := range rows {
		if got := forkPhotoAsk(r.l, r.text); got != r.hit {
			t.Errorf("forkPhotoAsk(%s, %q) = %v, want %v", r.l, r.text, got, r.hit)
		}
	}
}

// photoBot is the robot side of a camera turn: control, the interrupter's event stream, mirror mode, photo, speech.
// A closed wake fires one wake-word event on the stream; noPhoto fails the capture; capture n's JPEG ends in byte n.
type photoBot struct {
	controlBot
	smu      sync.Mutex
	said     []string
	wake     chan struct{}
	noPhoto  bool
	captures int
}

func (b *photoBot) EventStream(ctx context.Context, _ *vectorpb.EventRequest, _ ...grpc.CallOption) (vectorpb.ExternalInterface_EventStreamClient, error) {
	if b.wake != nil {
		return &wakeStream{ctx: ctx, wake: b.wake}, nil
	}
	return &eventStream{ctx: ctx, recvErr: make(chan error, 1)}, nil
}

// wakeStream sends the interrupter its baseline robot state, then one wake word once wake closes.
type wakeStream struct {
	vectorpb.ExternalInterface_EventStreamClient
	ctx        context.Context
	wake       chan struct{}
	sent, woke bool
}

func (s *wakeStream) Recv() (*vectorpb.EventResponse, error) {
	if !s.sent {
		s.sent = true
		return &vectorpb.EventResponse{Event: &vectorpb.Event{EventType: &vectorpb.Event_RobotState{RobotState: &vectorpb.RobotState{TouchData: &vectorpb.TouchData{}}}}}, nil
	}
	if !s.woke {
		select {
		case <-s.wake:
			s.woke = true
			return &vectorpb.EventResponse{Event: &vectorpb.Event{EventType: &vectorpb.Event_WakeWord{WakeWord: &vectorpb.WakeWord{}}}}, nil
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
	<-s.ctx.Done()
	return nil, s.ctx.Err()
}

func (s *wakeStream) CloseSend() error { return nil }

func (b *photoBot) EnableMirrorMode(context.Context, *vectorpb.EnableMirrorModeRequest, ...grpc.CallOption) (*vectorpb.EnableMirrorModeResponse, error) {
	return &vectorpb.EnableMirrorModeResponse{}, nil
}

func (b *photoBot) CaptureSingleImage(context.Context, *vectorpb.CaptureSingleImageRequest, ...grpc.CallOption) (*vectorpb.CaptureSingleImageResponse, error) {
	b.smu.Lock()
	b.captures++
	n := b.captures
	b.smu.Unlock()
	if b.noPhoto {
		return nil, errors.New("no camera")
	}
	return &vectorpb.CaptureSingleImageResponse{Data: []byte{0xff, 0xd8, 0xff, byte(n)}}, nil
}

func (b *photoBot) PlayAnimation(context.Context, *vectorpb.PlayAnimationRequest, ...grpc.CallOption) (*vectorpb.PlayAnimationResponse, error) {
	return &vectorpb.PlayAnimationResponse{}, nil
}

func (b *photoBot) SayText(_ context.Context, in *vectorpb.SayTextRequest, _ ...grpc.CallOption) (*vectorpb.SayTextResponse, error) {
	b.smu.Lock()
	defer b.smu.Unlock()
	b.said = append(b.said, in.Text)
	return &vectorpb.SayTextResponse{State: vectorpb.SayTextResponse_FINISHED}, nil
}

// photoRig is an et camera turn's fakes: the brain + vision LLM fake, the robot forkPhotoRobot dials, forkVisionSay's
// lines (each with the LLM events before it), the intent answers forkPhotoIntent sent, and an empty chat history.
type photoRig struct {
	f       *fakeLLM
	bot     *photoBot
	dials   int
	mu      sync.Mutex
	lines   []string
	intents []string
	fires   int
	onSay   func(line string, robot *vector.Vector) // runs after a line is noted
}

func usePhotoRig(t *testing.T) *photoRig {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	oldPath, oldURL := voice.SettingsPath, voice.URL
	voice.SettingsPath, voice.URL = func() string { return path }, "http://127.0.0.1:1"
	t.Cleanup(func() { voice.SettingsPath, voice.URL = oldPath, oldURL })
	if err := voice.Save(voice.Settings{T1: voice.Stock, T2: voice.Stock, T1ET: "mari", T2ET: "mari", Slow: 1}); err != nil {
		t.Fatal(err)
	}
	r := &photoRig{f: newFakeLLM(t), bot: &photoBot{}}
	r.f.replies = map[string][]string{"estllm:x": {"What do you see?", "Valge tass."}, llm.DefaultVisionET: {"A white cup."}}
	useAnthropic(t, r.f.URL)
	useKnowledge(t, "custom", "", "")
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: r.f.URL, Profiles: map[string]llm.Profile{lang.Estonian: {Provider: llm.Ollama, Model: "estllm:x"}}})
	freshListening(t)
	oldChats := vars.RememberedChats
	vars.RememberedChats = nil
	oldRobot, oldSay, oldIntent, oldFire, oldPhotos := forkPhotoRobot, forkVisionSay, forkPhotoIntent, forkFire, forkPhotos
	forkPhotos = new(photo.Store)
	forkChain.Reset("00e60001")
	forkFire = func(*vector.Vector) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.fires++
	}
	forkPhotoRobot = func(esn string) (*vector.Vector, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.dials++
		v := &vector.Vector{Conn: r.bot}
		v.Cfg.SerialNo = esn // DoGetImage remembers under it
		return v, nil
	}
	forkVisionSay = func(line string, robot *vector.Vector) error {
		r.f.mu.Lock()
		ev := strings.Join(r.f.events, ",")
		r.f.mu.Unlock()
		r.mu.Lock()
		r.lines = append(r.lines, line+" @"+ev)
		on := r.onSay
		r.mu.Unlock()
		if on != nil {
			on(line, robot)
		}
		return nil
	}
	forkPhotoIntent = func(_ interface{}, intent, _ string, _ map[string]string, _ bool) (interface{}, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.intents = append(r.intents, intent)
		return nil, nil
	}
	t.Cleanup(func() {
		forkPhotoRobot, forkVisionSay, forkPhotoIntent, forkFire, forkPhotos = oldRobot, oldSay, oldIntent, oldFire, oldPhotos
		forkChain.Reset("00e60001")
		vars.RememberedChats = oldChats
	})
	return r
}

// chats counts the fake's brain hops and vision chats, the first brain body returned.
func (r *photoRig) chats() (brain []string, vision int) {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	for _, b := range r.f.bodies {
		if strings.Contains(b, `"model":"estllm:x"`) {
			brain = append(brain, b)
		} else if strings.Contains(b, `"model":"`+llm.DefaultVisionET+`"`) {
			vision++
		}
	}
	return brain, vision
}

// history waits up to 2s for esn's remembered chat to reach n messages (DoGetImage remembers at its stream EOF).
func history(esn string, n int) []openai.ChatCompletionMessage {
	for end := time.Now().Add(2 * time.Second); len(GetChat(esn).Chats) < n && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	return GetChat(esn).Chats
}

// An et photo phrase runs forkPhotoTurn (the StreamingKGSim hook's seam) on a knowledge request and on intent_graph's
// unmatched one: "Vaatan." first, then the relay's two brain hops on the (not bare) utterance and one vision chat — no
// getImage-deciding brain hop — the answer spoken, and the turn remembered as (ask, answer) only.
func TestForkPhotoAskCamera(t *testing.T) {
	for _, isKG := range []bool{true, false} {
		t.Run(fmt.Sprint("isKG=", isKG), func(t *testing.T) {
			r := usePhotoRig(t)
			if forkPhotoTurn(nil, "00e60001", isKG, "mis kell on?") || r.dials != 0 {
				t.Fatalf("a non-photo request ran the photo turn (dials %d)", r.dials)
			}
			if !forkPhotoTurn(nil, "00e60001", isKG, "Mida sa näed laual?") {
				t.Fatal("photo phrase not handled")
			}
			if strings.Join(r.lines, "|") != "Vaatan. @" {
				t.Errorf("V18 lines %q, want [\"Vaatan. @\"] (spoken before any LLM call)", r.lines)
			}
			want := map[bool]string{true: "", false: "intent_greeting_hello"}[isKG]
			if got := strings.Join(r.intents, ","); got != want {
				t.Errorf("intent answers %q, want %q", got, want)
			}
			if brain, vision := r.chats(); len(brain) != 2 || vision != 1 || !strings.Contains(brain[0], "Mida sa näed laual?") {
				t.Errorf("events %v, brain hops %d (want the relay's 2, the first on the utterance), vision chats %d", r.f.events, len(brain), vision)
			}
			r.bot.smu.Lock()
			if n := len(r.bot.said); n == 0 || r.bot.said[n-1] != "Valge tass." {
				t.Errorf("robot said %q, want the relay answer last", r.bot.said)
			}
			r.bot.smu.Unlock()
			h := history("00e60001", 2)
			if len(h) != 2 || h[0].Role != openai.ChatMessageRoleUser || h[0].Content != "Mida sa näed laual?" || h[0].MultiContent != nil ||
				h[1].Role != openai.ChatMessageRoleAssistant || h[1].Content != "Valge tass." {
				t.Errorf("history %+v, want [user Mida sa näed laual?, assistant Valge tass.]", h)
			}
		})
	}
}

// A wake word as "Vaatan." is spoken cuts the speech, and the cut stops DoGetImage before the capture and relay:
// no LLM call at all. The answer slot is released, so the next photo ask runs in full.
func TestForkPhotoAskInterrupt(t *testing.T) {
	r := usePhotoRig(t)
	r.bot.wake = make(chan struct{})
	r.onSay = func(_ string, robot *vector.Vector) {
		r.onSay = nil
		close(r.bot.wake)
		select {
		case <-forkSpeakCtx(robot).Done():
		case <-time.After(2 * time.Second):
			t.Error("the wake word never cut the speech")
		}
	}
	if !forkPhotoTurn(nil, "00e60001", true, "Mida sa näed?") {
		t.Fatal("photo phrase not handled")
	}
	if brain, vision := r.chats(); len(brain) != 0 || vision != 0 {
		t.Fatalf("interrupted turn: brain hops %d, vision chats %d, want 0 and 0 (events %v)", len(brain), vision, r.f.events)
	}
	r.bot = &photoBot{}
	done := make(chan bool)
	go func() { done <- forkPhotoTurn(nil, "00e60001", true, "Mida sa näed?") }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the next photo ask never got the answer slot")
	}
	if _, vision := r.chats(); vision != 1 {
		t.Errorf("next ask: vision chats %d, want 1", vision)
	}
}

// useVisionStream points the en camera turn straight at an Ollama (no relay, brain = vision model: no unloads) whose
// nth chat (from 1) streams SSE as script writes it; usePhotoRig first. Returns the chat count.
func useVisionStream(t *testing.T, script func(n int, w http.ResponseWriter)) func() int {
	var mu sync.Mutex
	chats := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		if r.URL.Path != "/v1/chat/completions" {
			return
		}
		mu.Lock()
		chats++
		n := chats
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		script(n, w)
	}))
	t.Cleanup(s.Close)
	saveLLM(t, lang.English, llm.Config{OllamaURL: s.URL, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Ollama, Model: llm.DefaultVisionEN}}})
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return chats
	}
}

func sseChunk(w http.ResponseWriter, choices string) {
	io.WriteString(w, `data: {"id":"x","object":"chat.completion.chunk","choices":`+choices+"}\n\n")
	w.(http.Flusher).Flush()
}

func sseDelta(w http.ResponseWriter, content string) {
	c, _ := json.Marshal(content)
	sseChunk(w, `[{"index":0,"delta":{"content":`+string(c)+`}}]`)
}

func sseDone(w http.ResponseWriter) { io.WriteString(w, "data: [DONE]\n\n") }

// logSince marks the UI and tray logs; the func waits up to d for s in either, logged after the mark.
func logSince() func(s string, d time.Duration) bool {
	mark := fmt.Sprintf("test mark %d", time.Now().UnixNano())
	logger.LogUI(mark)
	logger.Println(mark)
	return func(s string, d time.Duration) bool {
		for end := time.Now().Add(d); ; time.Sleep(5 * time.Millisecond) {
			for _, l := range []string{logger.ForkLogList(), logger.ForkLogTrayList()} {
				if i := strings.LastIndex(l, mark); i >= 0 && strings.Contains(l[i:], s) {
					return true
				}
			}
			if time.Now().After(end) {
				return false
			}
		}
	}
}

// photoTurn runs forkPhotoTurn on an en photo ask, false if it has not returned within d.
func photoTurn(esn string, d time.Duration) (done chan bool, ended bool) {
	done = make(chan bool, 1)
	go func() { done <- forkPhotoTurn(nil, esn, true, "What do you see?") }()
	select {
	case <-done:
		return done, true
	case <-time.After(d):
		return done, false
	}
}

func (r *photoRig) saidText() string {
	r.bot.smu.Lock()
	defer r.bot.smu.Unlock()
	return strings.Join(r.bot.said, "|")
}

// A wake word while the speak loop waits for the vision model's first sentence ends the turn at once (nothing woke
// that wait, so the turn held control and the slot); the sentence the model finishes after the interrupt is never
// spoken (it played over the next turn) and the cut-off turn is not remembered.
func TestForkPhotoAskInterruptMidStream(t *testing.T) {
	r := usePhotoRig(t)
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	useVisionStream(t, func(_ int, w http.ResponseWriter) {
		sseDelta(w, "A white")
		<-release
		sseDelta(w, " cup.")
		sseDone(w)
	})
	t.Cleanup(free) // before the server's Close, which waits for the handler
	r.bot.wake = make(chan struct{})
	seen := logSince()
	done, _ := photoTurn("00e60002", 0)
	if !seen("Waiting for more content from LLM...", 5*time.Second) {
		t.Fatal("the speak loop never waited on the vision stream")
	}
	close(r.bot.wake)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("the interrupt did not end the turn while the vision model was mid-sentence")
		free()
		<-done
	}
	free()
	if !seen("LLM response for 00e60002: A white cup.", 5*time.Second) {
		t.Fatal("the vision stream never reached EOF")
	}
	if s := r.saidText(); strings.Contains(s, "cup") {
		t.Errorf("robot said %q: the vision sentence finished after the interrupt was spoken", s)
	}
	if h := GetChat("00e60002").Chats; len(h) != 0 {
		t.Errorf("history %+v, want none (the turn was interrupted)", h)
	}
	if got := strings.Join(r.lines, "|"); got != "Let me look. @" {
		t.Errorf("lines %q, want only the V18 line", got)
	}
}

// A vision stream that breaks mid-reply ends the turn (the speak loop waited forever, holding control and the answer
// slot): with no sentence spoken yet the V8 line follows, a sentence already spoken stands alone. The broken turn is
// not remembered; the next photo ask gets the slot.
func TestForkPhotoStreamBroken(t *testing.T) {
	for _, c := range []struct {
		name, sent, said, lines string
	}{
		{"before a sentence", "A white", "", "Let me look. @|I can't see right now. @|Let me look. @"},
		{"after a sentence", "A white cup. It is", "A white cup.", "Let me look. @|Let me look. @"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := usePhotoRig(t)
			chats := useVisionStream(t, func(n int, w http.ResponseWriter) {
				if n > 1 {
					sseDelta(w, "A red ball.")
					sseDone(w)
					return
				}
				sseDelta(w, c.sent)
				panic(http.ErrAbortHandler) // the connection drops mid-stream
			})
			if _, ok := photoTurn("00e60003", 5*time.Second); !ok {
				t.Fatal("a broken vision stream never ended the turn")
			}
			if s := r.saidText(); !strings.HasSuffix(s, "1"+map[bool]string{true: "|" + c.said, false: ""}[c.said != ""]) {
				t.Errorf("robot said %q, want the countdown then %q", s, c.said)
			}
			if _, ok := photoTurn("00e60003", 5*time.Second); !ok {
				t.Fatal("the next photo ask never got the answer slot")
			}
			if got := strings.Join(r.lines, "|"); got != c.lines {
				t.Errorf("lines %q, want %q", got, c.lines)
			}
			if h := history("00e60003", 2); chats() != 2 || len(h) != 2 || h[1].Content != "A red ball." {
				t.Errorf("chats %d, history %+v, want 2 and only the next ask's (What do you see?, A red ball.)", chats(), h)
			}
		})
	}
}

// An empty vision reply — no content, or only choice-less chunks (a usage chunk) — panicked DoGetImage's stream
// goroutine (fullRespSlice[0], Choices[0]); it speaks the V8 line and remembers nothing.
func TestForkPhotoEmptyReply(t *testing.T) {
	for _, c := range []struct {
		name string
		send func(w http.ResponseWriter)
	}{
		{"no content", func(w http.ResponseWriter) { sseDelta(w, ""); sseDone(w) }},
		{"no choices", func(w http.ResponseWriter) { sseChunk(w, "[]"); sseDone(w) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := usePhotoRig(t)
			useVisionStream(t, func(_ int, w http.ResponseWriter) { c.send(w) })
			if _, ok := photoTurn("00e60004", 5*time.Second); !ok {
				t.Fatal("an empty vision reply never ended the turn")
			}
			if got := strings.Join(r.lines, "|"); got != "Let me look. @|I can't see right now. @" {
				t.Errorf("lines %q, want the V18 then the V8 line", got)
			}
			if h := GetChat("00e60004").Chats; len(h) != 0 {
				t.Errorf("history %+v, want none", h)
			}
		})
	}
}

// A failed capture speaks the V8 line and makes no vision call (DoGetImage read a nil response's Data).
func TestForkPhotoCaptureFails(t *testing.T) {
	r := usePhotoRig(t)
	r.bot.noPhoto = true
	if !forkPhotoTurn(nil, "00e60001", true, "Mida sa näed?") {
		t.Fatal("photo phrase not handled")
	}
	if brain, vision := r.chats(); len(brain) != 0 || vision != 0 {
		t.Errorf("failed capture: brain hops %d, vision chats %d, want 0 and 0", len(brain), vision)
	}
	if got := strings.Join(r.lines, "|"); got != "Vaatan. @|Ma ei näe praegu. @" {
		t.Errorf("lines %q, want the V18 then the V8 line", got)
	}
}

// forkPhotoRemember with SaveChat on: a phrase turn appends (ask, answer); a brain-decided getImage extends the
// brain's remembered (ask, reply) pair; each captures one memory episode with the ask as User; no ask, no history.
func TestForkPhotoRemember(t *testing.T) {
	root := useMemory(t, "")
	old := vars.RememberedChats
	t.Cleanup(func() { vars.RememberedChats = old })
	sys := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: "sys"}
	ask := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "Mida sa näed?"}
	img := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, MultiContent: []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeImageURL}}}
	reply := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "Kohe vaatan."}
	pair := func(a string) []openai.ChatCompletionMessage {
		return []openai.ChatCompletionMessage{ask, {Role: openai.ChatMessageRoleAssistant, Content: a}}
	}
	for _, c := range []struct {
		name       string
		before     []openai.ChatCompletionMessage
		msgs       []openai.ChatCompletionMessage
		want       []openai.ChatCompletionMessage
		episodeAdd int
	}{
		{"phrase", nil, []openai.ChatCompletionMessage{sys, ask, img}, pair("Valge tass."), 1},
		{"brain", pair("Kohe vaatan."), []openai.ChatCompletionMessage{sys, ask, reply, img}, pair("Kohe vaatan. Valge tass."), 1},
		{"no ask", nil, []openai.ChatCompletionMessage{sys, img}, nil, 0},
	} {
		vars.RememberedChats = []vars.RememberedChat{{ESN: "esn", Chats: c.before}}
		n := len(episodes(t, root))
		if !forkPhotoRemember(c.msgs, "Valge tass.", "esn", nil) {
			t.Errorf("%s: false, want true (upstream's image Remember must not run)", c.name)
		}
		if got := GetChat("esn").Chats; !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: history %+v, want %+v", c.name, got, c.want)
		}
		e := episodes(t, root)
		if len(e)-n != c.episodeAdd || (c.episodeAdd == 1 && (e[len(e)-1].User != "Mida sa näed?" || e[len(e)-1].Reply != "Valge tass.")) {
			t.Errorf("%s: episodes %+v, want %d new with User = the ask", c.name, e[n:], c.episodeAdd)
		}
	}
}

// The fork hooks in upstream files the tests above drive through their seams.
func TestForkPhotoAskHook(t *testing.T) {
	for _, h := range []struct{ file, fn, end, line string }{
		{"kgsim.go", "func StreamingKGSim(", "\n\tif skip {", "\n\tskip = skip || forkPhotoTurn(req, esn, isKG, transcribedText)"},
		{"kgsim_cmds.go", "func DoGetImage(", "\n\t// add image to messages", "\n\treqBase64 := base64.StdEncoding.EncodeToString(forkPhotoData(resp, robot, &stopImaging))"},
		{"kgsim_cmds.go", "func DoGetImage(", "\n\t\t\t\t\tRemember(msgs[len(msgs)-1],", "\n\t\t\t\tif !forkPhotoRemember(msgs, newStr, robot.Cfg.SerialNo, stopped) {"},
		{"kgsim_cmds.go", "func DoGetImage(", "\n\t\t\t\t\tisDone = true", "\n\t\t\t\tforkPhotoAnswered(fullRespSlice, robot.Cfg.SerialNo, stopped)"},
		{"matchIntentSend.go", "func IntentPass(", "\n\t// intercept if not intent graph", "\n\tforkIntentPass(esn, intentThing, speechText)"},
	} {
		b, err := os.ReadFile(h.file)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		i := strings.Index(s, h.fn)
		if i < 0 {
			t.Fatalf("%s: no %q", h.file, h.fn)
		}
		body := s[i:]
		j := strings.Index(body, h.end)
		if j < 0 {
			t.Fatalf("%s: %s has no %q — the hook's anchor moved", h.file, h.fn, h.end)
		}
		if !strings.Contains(body[:j], h.line) {
			t.Errorf("%s: %s lacks the hook %q before %q", h.file, h.fn, h.line, h.end)
		}
	}
}
