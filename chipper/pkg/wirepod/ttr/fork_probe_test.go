package wirepod_ttr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func TestForkProbeLLMFull(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"It is *eight*. And then","reasoning":"they ask the time"},"finish_reason":"length"}],"usage":{"completion_tokens":9}}`)
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
}
