package wirepod_ttr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// forkETLine is the language line the Stage 1.3 bench scored EstLLM with (robot/et-llm/bench.py).
const forkETLine = "\n\nAnswer in Estonian."

func forkKG() llm.KG {
	k := vars.APIConfig.Knowledge
	return llm.KG{Provider: k.Provider, Endpoint: k.Endpoint, Model: k.Model}
}

// forkNow is the request clock; a var so tests pin it.
var forkNow = time.Now

// forkBotZone is the robot's vic.RobotSettings time_zone (what its clock shows), else time.Local.
func forkBotZone(esn string) *time.Location {
	var s struct {
		TimeZone string `json:"time_zone"`
	}
	if j, ok := vars.GetJdoc("vic:"+esn, "vic.RobotSettings"); ok {
		json.Unmarshal([]byte(j.JsonDoc), &s)
	}
	return prompt.Zone(s.TimeZone)
}

// forkAIReq: the system prompt gets the prompt.Now line, then (et-EE) forkETLine; the llm.Resolve target sets the
// model (E25) and its top_p (CreateAIReq set it for the Knowledge model). Ollama gets reasoning_effort none (qwen3.5/gemma4 think by default).
func forkAIReq(r openai.ChatCompletionRequest, esn string) openai.ChatCompletionRequest {
	l := lang.Current()
	if len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		r.Messages[0].Content += "\n\n" + prompt.Now(l, forkNow(), forkBotZone(esn))
		if l == lang.Estonian {
			r.Messages[0].Content += forkETLine
		}
	}
	kg := forkKG()
	ollama := llm.IsOllama(kg)
	if t, ok := llm.Resolve(llm.Load(), l, kg); ok {
		r.Model = t.Model
		r.TopP = forkTargetTopP(t.Provider, r.Model)
		ollama = t.Ollama()
	}
	if ollama {
		r.ReasoningEffort = "none"
	}
	return r
}

// forkCameraReq readies the getImage follow-up; defer the returned func. et-EE on Ollama (E26): the ET model is
// unloaded first and the vision model (llm.VisionModel) after, both at the target's Ollama, so one LLM stays resident.
func forkCameraReq(r *openai.ChatCompletionRequest) func() {
	l, c, kg := lang.Current(), llm.Load(), forkKG()
	t, ok := llm.Resolve(c, l, kg)
	if !ok {
		if kg.Provider == "openai" && kg.Model != "" {
			r.Model = kg.Model // upstream DoGetImage hardcodes GPT4oMini for openai
		}
		if llm.IsOllama(kg) {
			r.ReasoningEffort = "none"
		}
		return func() {}
	}
	r.Model = t.Model
	if t.Ollama() {
		r.ReasoningEffort = "none"
	}
	if !t.Ollama() || l != lang.Estonian {
		r.TopP = forkTargetTopP(t.Provider, r.Model)
		return func() {}
	}
	base, vision := t.OllamaBase(), llm.VisionModel(c, kg)
	forkUnload(base, t.Model)
	r.Model = vision
	r.TopP = forkTopP(r.Model)
	return func() { forkUnload(base, vision) }
}

// forkTargetTopP: a Claude target never sends top_p, whatever its model ID.
func forkTargetTopP(provider, model string) float32 {
	if provider == llm.Claude {
		return 0
	}
	return forkTopP(model)
}

// forkUnload asks the Ollama at base to drop model from memory now (native /api/generate, keep_alive 0).
func forkUnload(base, model string) {
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
