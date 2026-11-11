package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
)

func TestForkProbeLLMFull(t *testing.T) {
	var bodies []string
	var reply atomic.Value
	reply.Store(`{"choices":[{"message":{"role":"assistant","content":"It is *eight*. And then","reasoning":"they ask the time"},"finish_reason":"length"}],"usage":{"completion_tokens":9}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, reply.Load().(string))
	}))
	t.Cleanup(srv.Close)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	vars.APIConfig.Knowledge.Enable, vars.APIConfig.Knowledge.IntentGraph = true, true
	useBotClock(t, "Europe/Tallinn")
	saveLLM(t, lang.English, llm.Config{OllamaURL: srv.URL, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Ollama, Model: "qwen3.5:4b"}}})
	r := CreateAIReq("what time is it?", "esn", false, false)
	r.Stream = false
	if _, err := forkClient(nil).CreateChatCompletion(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	res, err := ForkProbeLLMFull(context.Background(), "what time is it?", "esn")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Errorf("probe request differs from production:\n%s\n%s", bodies[0], bodies[len(bodies)-1])
	}
	if res.Reasoning != "they ask the time" || res.RawAnswer != "It is *eight*. And then" || res.Answer == res.RawAnswer || res.Usage.CompletionTokens != 9 {
		t.Errorf("result = %+v", res)
	}

	old := vars.RememberedChats // G2: a remembered question is dropped as live drops it from speech and memory
	t.Cleanup(func() { vars.RememberedChats = old })
	vars.RememberedChats = nil
	q := "Kas sinuk on mingid eriti huvilised tegurid?"
	Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: "tere"},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "Tere! " + q}, "esn")
	reply.Store(`{"choices":[{"message":{"role":"assistant","content":"Hea kull. ` + q + ` See on hea."},"finish_reason":"stop"}]}`)
	if res, err = ForkProbeLLMFull(context.Background(), "jah", "esn"); err != nil || res.Answer != "Hea kull. See on hea." {
		t.Errorf("repeated question: answer %q (err %v), want %q", res.Answer, err, "Hea kull. See on hea.")
	}
}

// TestForkProbeSpoken: a whole reply kept as the stream loop keeps it — G2 drops, then the EOF cut/held/unpunctuated
// flushes, in the stream's order.
func TestForkProbeSpoken(t *testing.T) {
	q := "Kas sinuk on mingid eriti huvilised tegurid?"
	for _, c := range []struct {
		name, answer string
		cut          bool
		want         string
	}{
		{"no repeat", "Hea küll.\nSee on hea. Ja", false, "Hea küll. See on hea. Ja"},
		{"repeat mid-reply", "Hea küll. " + q + " See on hea.", false, "Hea küll. See on hea."},
		{"repeat last", "Hea küll. " + q, false, "Hea küll."},
		{"only the repeat", q, false, ""},
		{"cut after a kept sentence", "Hea küll. " + q + " Ja siis", true, "Hea küll."},
		{"cut, every sentence a repeat: says nothing", q + " Ja siis", true, ""},
		{"cut, nothing said: the fragment is spoken", "Ja siis", true, "Ja siis"},
		{"held digit", "Hea küll. " + q + " Kell on 10.", false, "Hea küll. Kell on 10."},
	} {
		rep := &forkRepeats{said: []string{"Tere! " + q}}
		if got := forkProbeSpoken(c.answer, c.cut, rep); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// A templated et weather turn answers the probe with its canned reply and no LLM call.
func TestForkProbeLLMCanned(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	t.Cleanup(srv.Close)
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	vars.APIConfig.Knowledge.Enable, vars.APIConfig.Knowledge.IntentGraph = true, true
	useBotClock(t, "Europe/Tallinn")
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: srv.URL, Profiles: map[string]llm.Profile{lang.Estonian: {Provider: llm.Ollama, Model: "qwen3.5:4b"}}})
	oldS, oldW := forkSearch, forkWeatherTemplate
	t.Cleanup(func() { forkSearch, forkWeatherTemplate = oldS, oldW; forkCanned.Delete("esn") })
	forkSearch = func(string, []string, string, string, *time.Location) string { return "" }
	forkWeatherTemplate = func(string, string, func(string) string, *time.Location) (string, bool) {
		return "Homme on Tallinnas kaks kraadi sooja.", true
	}
	res, err := ForkProbeLLMFull(context.Background(), "kas homme sajab?", "esn")
	if err != nil || res.RawAnswer != "Homme on Tallinnas kaks kraadi sooja." || calls != 0 {
		t.Errorf("probe = %+v, %v after %d LLM calls; want the canned reply, no call", res, err, calls)
	}
}
