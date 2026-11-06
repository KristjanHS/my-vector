package wirepod_ttr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// forkETLine is the language line the Stage 1.3 bench scored EstLLM with (robot/et-llm/bench.py).
const forkETLine = "\n\nAnswer in Estonian."

func forkKG() llm.KG {
	k := vars.APIConfig.Knowledge
	return llm.KG{Provider: k.Provider, Endpoint: k.Endpoint, Model: k.Model}
}

// forkAIReq: et-EE adds forkETLine to the system prompt; the llm.Resolve target sets the model (E25).
// Ollama always gets reasoning_effort none (qwen3.5/gemma4 think by default; OpenAI rejects it).
func forkAIReq(r openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	l := lang.Current()
	if l == lang.Estonian && len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		r.Messages[0].Content += forkETLine
	}
	kg := forkKG()
	ollama := llm.IsOllama(kg)
	if t, ok := llm.Resolve(llm.Load(), l, kg); ok {
		r.Model = t.Model
		ollama = t.Ollama()
	}
	if ollama {
		r.ReasoningEffort = "none"
	}
	return r
}

// forkCameraReq readies the getImage follow-up; defer the returned func. et-EE on Ollama (E26): the ET
// model is unloaded first and r.Model (the vision model) after, so one LLM stays resident.
func forkCameraReq(r *openai.ChatCompletionRequest) func() {
	kg := forkKG()
	if !llm.IsOllama(kg) {
		return func() {}
	}
	r.ReasoningEffort = "none"
	l := lang.Current()
	t, ok := llm.Resolve(llm.Load(), l, kg)
	if !ok || l != lang.Estonian {
		return func() {}
	}
	forkUnload(t.Model)
	m := r.Model
	return func() { forkUnload(m) }
}

// forkUnload asks Ollama to drop model from memory now (native /api/generate, keep_alive 0).
func forkUnload(model string) {
	base := strings.TrimSuffix(strings.TrimRight(vars.APIConfig.Knowledge.Endpoint, "/"), "/v1")
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Post(base+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Println("myvector: unload " + model + ": " + err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Println("myvector: unload " + model + ": " + resp.Status)
	}
}
