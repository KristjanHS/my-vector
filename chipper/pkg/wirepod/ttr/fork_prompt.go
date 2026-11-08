package wirepod_ttr

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

func forkKG() llm.KG {
	k := vars.APIConfig.Knowledge
	return llm.KG{Provider: k.Provider, Endpoint: k.Endpoint, Model: k.Model}
}

// forkNow is the request clock; a var so tests pin it.
var forkNow = time.Now

// forkSearch fills {search} from the last user message; a var so tests fake it.
var forkSearch = search.Fill

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

// forkAIReq: the llm.Resolve target sets the model (E25), then llm.SamplingFor sets that model's sampling;
// then the brain's rendered template replaces the system prompt and its ReplyCap (LongReplyCap for isKG or a ForkLongAsk) caps the reply.
// Ollama gets llm.Config.Thinking for PathChat: reasoning_effort, the reply cap + its budget, its sampling (none by default).
func forkAIReq(r openai.ChatCompletionRequest, esn string, isKG bool) openai.ChatCompletionRequest {
	l := lang.Current()
	kg := forkKG()
	ollama := llm.IsOllama(kg)
	provider := ""
	cfg := llm.Load()
	if t, ok := forkResolve(cfg, l, kg); ok {
		r.Model, provider = t.Model, t.Provider
		ollama = t.Ollama()
	}
	llm.SamplingFor(provider, r.Model).Apply(&r)
	brain := forkBrain(ollama)
	long := isKG
	if n := len(r.Messages); n > 0 && !long {
		long = vars.ForkLongAsk(r.Messages[n-1].Content)
	}
	forkReplyCap(&r, ollama, long)
	if ollama {
		cfg.Thinking(llm.Ollama, r.Model, llm.PathChat).Apply(&r, r.MaxTokens)
	}
	if long {
		logger.Println("myvector: reply cap", prompt.LongReplyCap(brain), "(long ask)")
	}
	if len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		s := prompt.Load()
		last := r.Messages[len(r.Messages)-1].Content
		memory.MarkLLM(esn, last)
		memory.Touch()
		facts, diary := forkMemory(esn, last, brain)
		r.Messages[0].Content = prompt.Render(s.Live(brain), prompt.Vars{
			Now:      prompt.Now(l, forkNow(), forkBotZone(esn)),
			Lang:     prompt.LangLine(l),
			User:     s.User(),
			Facts:    facts,
			Diary:    diary,
			Search:   forkSearch(last, l, brain, forkBotZone(esn)),
			Speech:   prompt.SpeechBlock(),
			Commands: forkCommands(isKG),
		})
		if os.Getenv("DEBUG_PRINT_PROMPT") == "true" {
			logger.Println(r.Messages[0].Content)
		}
	}
	return r
}

// forkReplyCap sets the brain's ReplyCap (LongReplyCap when long): Ollama's /v1 honours only max_tokens (0.40.14 ignores max_completion_tokens).
func forkReplyCap(r *openai.ChatCompletionRequest, ollama, long bool) {
	n := prompt.ReplyCap(forkBrain(ollama))
	if long {
		n = prompt.LongReplyCap(forkBrain(ollama))
	}
	if ollama {
		r.MaxTokens, r.MaxCompletionTokens = n, 0
	} else {
		r.MaxTokens, r.MaxCompletionTokens = 0, n
	}
}

func forkBrain(ollama bool) string {
	if ollama {
		return prompt.Ollama
	}
	return prompt.Claude
}

// forkCommands is the {commands} block: empty unless CommandsEnable; newVoiceRequest only on the KG path with SaveChat.
func forkCommands(isKG bool) string {
	k := vars.APIConfig.Knowledge
	if !k.CommandsEnable {
		return ""
	}
	emotions := prompt.SampleEmotions
	for _, c := range ValidLLMCommands {
		if c.Command == "playAnimationWI" {
			emotions = c.ParamChoices
		}
	}
	return prompt.Commands(emotions, isKG && k.SaveChat)
}

// forkCameraReq readies the getImage follow-up on forkVision's target and swaps *cl to it; defer the returned
// func. et-EE on Ollama (E26): the ET model is unloaded first and the vision model after, so one LLM stays resident.
func forkCameraReq(r *openai.ChatCompletionRequest, cl **openai.Client) func() {
	l, c, kg := lang.Current(), llm.Load(), forkKG()
	t, ok := forkResolve(c, l, kg)
	forkReplyCap(r, (ok && t.Ollama()) || (!ok && llm.IsOllama(kg)), false)
	if !ok {
		if kg.Provider == "openai" && kg.Model != "" {
			r.Model = kg.Model // upstream DoGetImage hardcodes GPT4oMini for openai
		}
		if llm.IsOllama(kg) {
			r.ReasoningEffort = "none"
		}
		llm.SamplingFor("", r.Model).Apply(r)
		return func() {}
	}
	v, vok := forkVision(c, l, kg, t)
	if !vok {
		logger.Println("myvector llm: camera " + l + " has no vision target — asking the brain " + t.Model)
		v = t
	}
	logger.Println("myvector llm: camera " + l + " → " + v.Provider + " " + v.Model + " @ " + v.BaseURL)
	*cl = llm.NewClient(v)
	r.Model = v.Model
	if v.Ollama() {
		r.ReasoningEffort = "none"
	}
	llm.SamplingFor(v.Provider, v.Model).Apply(r)
	if !t.Ollama() || l != lang.Estonian {
		return func() {}
	}
	base := t.OllamaBase()
	forkUnload(base, t.Model)
	return func() { forkUnload(base, v.Model) }
}

// forkVision is llm.ResolveVision on the already-resolved brain t (one listening read decides both): et-EE on Ollama
// keeps t's base and takes the vision model; false = no vision target (a copied profile map: the saved one is untouched).
func forkVision(c llm.Config, l string, kg llm.KG, t llm.Target) (llm.Target, bool) {
	if !t.Ollama() || l != lang.Estonian {
		return t, true
	}
	ps := map[string]llm.Profile{l: {Provider: llm.Ollama, Model: t.Model}}
	for k, p := range c.Profiles {
		if k != l {
			ps[k] = p
		}
	}
	c.Profiles = ps
	w, ok := llm.ResolveVision(c, l, kg)
	if !ok || !w.Ollama() {
		return llm.Target{}, false
	}
	t.Model = w.Model
	return t, true
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
