package wirepod_ttr

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	openai "github.com/sashabaranov/go-openai"
)

type fakeDeltas []string

func (f *fakeDeltas) Recv() (openai.ChatCompletionStreamResponse, error) {
	if len(*f) == 0 {
		return openai.ChatCompletionStreamResponse{}, io.EOF
	}
	c := (*f)[0]
	*f = (*f)[1:]
	return openai.ChatCompletionStreamResponse{Choices: []openai.ChatCompletionStreamChoice{{Delta: openai.ChatCompletionStreamChoiceDelta{Content: c}}}}, nil
}

func usePrefillFakes(t *testing.T, block string, deltas ...string) *[]openai.ChatCompletionRequest {
	t.Helper()
	oldS, oldF := forkSearch, forkOpenStream
	t.Cleanup(func() { forkSearch, forkOpenStream = oldS, oldF; forkPrefill.Delete("esn") })
	forkSearch = func(string, []string, string, string, *time.Location) string { return block }
	var sent []openai.ChatCompletionRequest
	forkOpenStream = func(_ *openai.Client, _ context.Context, req openai.ChatCompletionRequest) (forkRecv, error) {
		sent = append(sent, req)
		d := fakeDeltas(deltas)
		return &d, nil
	}
	return &sent
}

func spokenText(t *testing.T, s forkRecv) string {
	t.Helper()
	var out string
	for {
		r, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out += r.Choices[0].Delta.Content
	}
}

// An OllamaEt news ask with a web block streams after the opener, which is also spoken; the request keeps no prefill.
func TestForkAIReqPrefill(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	useBotClock(t, "Europe/Tallinn")
	sent := usePrefillFakes(t, "Värsked veebitulemused (kuupäev, allikas):\n- ERR 10.11: Nvidia aktsia langes", "", "on Nvidia aktsia langenud.", " Lõpp.")
	ask := "otsi uudiseid Nvidia aktsia kohta"
	req := CreateAIReq(ask, "esn", false, false)
	n := len(req.Messages)
	s, err := forkLLMStream(nil, context.Background(), req, "esn")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := spokenText(t, s), forkNewsOpener+" on Nvidia aktsia langenud. Lõpp."; got != want {
		t.Errorf("spoken %q, want %q", got, want)
	}
	if len(*sent) != 1 {
		t.Fatalf("%d LLM calls, want 1", len(*sent))
	}
	m := (*sent)[0].Messages
	if len(m) != n+1 || m[n].Role != openai.ChatMessageRoleAssistant || m[n].Content != forkNewsOpener || m[n-1].Content != req.Messages[n-1].Content {
		t.Errorf("sent messages end %+v, want the ask then the assistant opener", m[len(m)-2:])
	}
	if len(req.Messages) != n || req.Messages[n-1].Role == openai.ChatMessageRoleAssistant {
		t.Error("the caller's request (its chat history) must keep no prefill")
	}
	if _, ok := forkPrefill.Load("esn"); ok {
		t.Error("the stream must consume the opener")
	}
}

// No opener without a web block, for a non-news web ask, in en, or for another ask's stream.
func TestForkPrefillSkips(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	useBotClock(t, "Europe/Tallinn")
	news := "otsi uudiseid Nvidia aktsia kohta"
	for name, c := range map[string]struct{ block, ask string }{
		"no block":    {"", news},
		"unavailable": {search.Unavailable(lang.Estonian), news},
		"non-news":    {"Värsked veebitulemused:\n- rimi.ee: avatud 8–22", "millal Rimi lahti on?"},
	} {
		usePrefillFakes(t, c.block)
		CreateAIReq(c.ask, "esn", false, false)
		if _, ok := forkPrefill.Load("esn"); ok {
			t.Errorf("%s: want no opener", name)
		}
	}

	sent := usePrefillFakes(t, "Värsked veebitulemused:\n- ERR", "Vastus.")
	CreateAIReq(news, "esn", false, false)
	other := openai.ChatCompletionRequest{Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "räägi nali"}}}
	s, _ := forkLLMStream(nil, context.Background(), other, "esn")
	if got := spokenText(t, s); got != "Vastus." || len((*sent)[0].Messages) != 1 {
		t.Errorf("another ask's stream: spoken %q, %d messages; want no opener", got, len((*sent)[0].Messages))
	}

	lang.UseForTest(t, lang.English)
	usePrefillFakes(t, "Fresh web results:\n- Reuters")
	CreateAIReq("search news about Nvidia stocks", "esn", false, false)
	if _, ok := forkPrefill.Load("esn"); ok {
		t.Error("en brain: want no opener")
	}
}

// A delta starting with punctuation or a space joins the opener without an added space.
func TestForkPrefixStreamJoin(t *testing.T) {
	for in, want := range map[string]string{"on hea.": "X on hea.", ", et hea.": "X, et hea.", " hea.": "X hea.", "„Postimees”": "X „Postimees”"} {
		d := fakeDeltas{in}
		if got := spokenText(t, &forkPrefixStream{s: &d, text: "X"}); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
