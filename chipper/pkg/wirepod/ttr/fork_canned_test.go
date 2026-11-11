package wirepod_ttr

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	openai "github.com/sashabaranov/go-openai"
)

func TestForkCanned(t *testing.T) {
	old := forkWeatherTemplate
	t.Cleanup(func() { forkWeatherTemplate = old; forkCanned.Delete("e1") })
	var in string
	forkWeatherTemplate = func(s, l string, inessive func(string) string, _ *time.Location) (string, bool) {
		in = inessive("Tallinn")
		return "Homme on Tallinnas kaks kraadi sooja.", s == "kas homme sajab?"
	}

	forkCannedSet("e1", "kas homme sajab?", lang.Estonian, prompt.Ollama)
	if _, ok := forkCanned.Load("e1"); ok {
		t.Error("en Ollama brain: want no canned reply")
	}
	forkCannedSet("e1", "kas homme sajab?", lang.Estonian, prompt.OllamaEt)
	if in != "Tallinnas" {
		t.Errorf("inessive(Tallinn) = %q, want Tallinnas", in)
	}
	ask := openai.ChatCompletionRequest{Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "kas homme sajab?"}}}
	s, err := forkLLMStream(nil, context.Background(), ask, "e1")
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Recv()
	if err != nil || len(r.Choices) != 1 || r.Choices[0].Delta.Content != "Homme on Tallinnas kaks kraadi sooja." ||
		r.Choices[0].FinishReason != openai.FinishReasonStop {
		t.Errorf("first Recv = %+v, %v", r, err)
	}
	if _, err := s.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("second Recv err %v, want io.EOF", err)
	}
	if _, ok := forkCanned.Load("e1"); ok {
		t.Error("the stream must consume the canned reply")
	}

	forkCannedSet("e1", "kas homme sajab?", lang.Estonian, prompt.OllamaEt)
	other := openai.ChatCompletionRequest{Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "räägi nali"}}}
	if _, ok := forkCannedStreamFor(other, "e1"); ok {
		t.Error("a stream for another ask must not speak the canned reply")
	}
	if _, ok := forkCanned.Load("e1"); !ok {
		t.Error("a mismatched stream must leave the entry for the next request build to set or clear")
	}

	// Overlapping requests: B's build stored its reply, then A's stream ran; B's entry survives for B's stream.
	forkCannedSet("e1", "kas homme sajab?", lang.Estonian, prompt.OllamaEt)
	a := openai.ChatCompletionRequest{Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "mis kell on?"}}}
	if _, ok := forkCannedStreamFor(a, "e1"); ok {
		t.Error("ask A's stream must not speak ask B's canned reply")
	}
	b := openai.ChatCompletionRequest{Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "kas homme sajab?"}}}
	if r, ok := forkCannedStreamFor(b, "e1"); !ok || r.text != "Homme on Tallinnas kaks kraadi sooja." {
		t.Errorf("ask B's stream after A's = %+v, %v; want B's canned reply", r, ok)
	}
	if _, ok := forkCanned.Load("e1"); ok {
		t.Error("B's stream must consume its canned reply")
	}

	forkCannedSet("e1", "kas homme sajab?", lang.Estonian, prompt.OllamaEt)
	forkCannedSet("e1", "räägi nali", lang.Estonian, prompt.OllamaEt)
	if _, ok := forkCanned.Load("e1"); ok {
		t.Error("a non-weather build must clear the previous canned reply")
	}
	if got := forkWeatherInessive("Atlantis"); got != "" {
		t.Errorf("unknown place inessive = %q, want \"\"", got)
	}
}

func TestForkSignalIntent(t *testing.T) {
	c := make(chan bool)
	forkSignalIntent(c) // the split's non-blocking sends were all dropped: nobody ranged c yet
	time.Sleep(20 * time.Millisecond)
	select {
	case v := <-c:
		if !v {
			t.Error("want true")
		}
	case <-time.After(time.Second):
		t.Fatal("a late reader must still get the stream's true")
	}
}

// CreateAIReq (forkAIReq) gives an OllamaEt weather ask its canned reply, keyed to that ask, after the search.
func TestForkAIReqCanned(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	useBotClock(t, "Europe/Tallinn")
	oldS, oldW := forkSearch, forkWeatherTemplate
	t.Cleanup(func() { forkSearch, forkWeatherTemplate = oldS, oldW; forkCanned.Delete("esn") })
	var order []string
	forkSearch = func(string, []string, string, string, *time.Location) string {
		order = append(order, "search")
		return ""
	}
	forkWeatherTemplate = func(s, l string, _ func(string) string, _ *time.Location) (string, bool) {
		order = append(order, "template")
		return "Homme on Tallinnas kaks kraadi sooja.", strings.Contains(s, "sajab")
	}
	req := CreateAIReq("kas homme sajab?", "esn", false, false)
	if strings.Join(order, ",") != "search,template" {
		t.Errorf("order %v, want search then template (the template reads the search's forecast memo)", order)
	}
	v, ok := forkCanned.Load("esn")
	if !ok || v.(forkCannedReply).ask != req.Messages[len(req.Messages)-1].Content {
		t.Fatalf("canned %+v %v, want one keyed to the request's last message", v, ok)
	}
	if r, ok := forkCannedStreamFor(req, "esn"); !ok || r.text != "Homme on Tallinnas kaks kraadi sooja." {
		t.Errorf("the request's own stream: %+v %v", r, ok)
	}
	CreateAIReq("räägi nali", "esn", false, false)
	if _, ok := forkCanned.Load("esn"); ok {
		t.Error("a non-weather request must clear the canned reply")
	}
}

// kgsim.go streams through forkLLMStream and signals successIntent after a chunked stream (an instant canned reply).
func TestForkCannedKgsimHooks(t *testing.T) {
	src, err := os.ReadFile("kgsim.go")
	if err != nil {
		t.Fatal(err)
	}
	for call, want := range map[string]int{"forkLLMStream(c, ctx, aireq, esn)": 1, "forkSignalIntent(successIntent)": 1,
		"c.CreateChatCompletionStream(": 1} { // 1: upstream's GPT-4 fallback
		n := 0
		for _, line := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") && strings.Contains(line, call) {
				n++
			}
		}
		if n != want {
			t.Errorf("kgsim.go has %d %s, want %d", n, call, want)
		}
	}
}
