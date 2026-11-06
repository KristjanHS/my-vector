package wirepod_ttr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// forkETLine is the language line the Stage 1.3 bench scored EstLLM with (robot/et-llm/bench.py).
const forkETLine = "\n\nAnswer in Estonian."

// forkOllamaPort marks the endpoint as Ollama (plan E27); tests point it at an httptest port.
var forkOllamaPort = "11434"

func forkOllama() bool {
	if vars.APIConfig.Knowledge.Provider != "custom" {
		return false
	}
	u, err := url.Parse(vars.APIConfig.Knowledge.Endpoint)
	return err == nil && u.Port() == forkOllamaPort
}

// forkAIReq: et-EE adds forkETLine to the system prompt and, on Ollama, swaps in lang.ETModel() (E25);
// Ollama always gets reasoning_effort none (qwen3.5/gemma4 think by default; OpenAI rejects it).
func forkAIReq(r openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	et := lang.Current() == lang.Estonian
	if et && len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		r.Messages[0].Content += forkETLine
	}
	if forkOllama() {
		r.ReasoningEffort = "none"
		if et {
			r.Model = lang.ETModel()
		}
	}
	return r
}

// forkCameraReq readies the getImage follow-up; defer the returned func. et-EE on Ollama (E26): the ET
// model is unloaded first and r.Model (the vision model) after, so one LLM stays resident.
func forkCameraReq(r *openai.ChatCompletionRequest) func() {
	if !forkOllama() {
		return func() {}
	}
	r.ReasoningEffort = "none"
	if lang.Current() != lang.Estonian {
		return func() {}
	}
	forkUnload(lang.ETModel())
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
