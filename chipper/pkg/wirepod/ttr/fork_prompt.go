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
// Without SaveChat the remembered chat still follows the system message (F3; upstream splices it with SaveChat).
func forkAIReq(r openai.ChatCompletionRequest, esn string, isKG bool) openai.ChatCompletionRequest {
	if h := GetChat(esn).Chats; !vars.APIConfig.Knowledge.SaveChat && len(h) > 0 && len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		r.Messages = append(append([]openai.ChatCompletionMessage{r.Messages[0]}, h...), r.Messages[1:]...)
	}
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
	brain := forkBrain(ollama, l)
	long := isKG
	if n := len(r.Messages); n > 0 && !long {
		long = vars.ForkLongAsk(r.Messages[n-1].Content)
	}
	forkReplyCap(&r, ollama, long, brain)
	if ollama {
		cfg.Thinking(llm.Ollama, r.Model, llm.PathChat).Apply(&r, r.MaxTokens)
	}
	if long {
		logger.Println("myvector: reply cap", prompt.LongReplyCap(brain), "(long ask)")
	}
	if len(r.Messages) > 0 && r.Messages[0].Role == openai.ChatMessageRoleSystem {
		last := r.Messages[len(r.Messages)-1].Content
		memory.MarkLLM(esn, last)
		memory.Touch()
		prev := forkPrevAsks(esn) // once: the search and the opener judge the same follow-up topic
		block := forkSearching(esn, last, l, brain, prev)
		r.Messages[0].Content = forkSystem(esn, last, l, brain, block, false)
		forkCannedSet(esn, last, l, brain) // after the search: its forecast memo serves the template (fork_canned.go)
		forkPrefillSet(esn, last, l, brain, block, prev)
		if os.Getenv("DEBUG_PRINT_PROMPT") == "true" {
			logger.Println(r.Messages[0].Content)
		}
	}
	return r
}

// forkSystem renders brain's live template for esn's utterance last in language l; photo: a photo answer's commands.
func forkSystem(esn, last, l, brain, search string, photo bool) string {
	s := prompt.Load()
	facts, diary := forkMemory(esn, last, brain)
	if search == "" && !photo {
		search = prompt.NoSearch(brain)
	}
	return prompt.Render(s.Live(brain), prompt.Vars{
		Now:      prompt.Now(l, forkNow(), forkBotZone(esn)),
		Lang:     prompt.LangLine(brain, l),
		User:     s.User(brain),
		Facts:    facts,
		Diary:    diary,
		Search:   search,
		Speech:   prompt.SpeechBlock(brain),
		Commands: forkCommands(brain, photo),
	})
}

// forkReplyCap sets the brain's ReplyCap (LongReplyCap when long): Ollama's /v1 honours only max_tokens (0.40.14 ignores max_completion_tokens).
func forkReplyCap(r *openai.ChatCompletionRequest, ollama, long bool, brain string) {
	n := prompt.ReplyCap(brain)
	if long {
		n = prompt.LongReplyCap(brain)
	}
	if ollama {
		r.MaxTokens, r.MaxCompletionTokens = n, 0
	} else {
		r.MaxTokens, r.MaxCompletionTokens = 0, n
	}
}

// forkBrain picks the brain by provider and reply language l.
func forkBrain(ollama bool, l string) string {
	if ollama {
		return prompt.OllamaFor(l)
	}
	return prompt.Claude
}

// forkCommands is brain's {commands} block: empty unless CommandsEnable (a photo answer's: PhotoLine alone).
func forkCommands(brain string, photo bool) string {
	k := vars.APIConfig.Knowledge
	if !k.CommandsEnable {
		if photo {
			return prompt.PhotoLine(brain)
		}
		return ""
	}
	emotions := prompt.SampleEmotions
	for _, c := range ValidLLMCommands {
		if c.Command == "playAnimationWI" {
			emotions = c.ParamChoices
		}
	}
	if photo {
		return prompt.PhotoCommands(brain, emotions)
	}
	return prompt.Commands(brain, emotions)
}

// forkCameraReq readies the getImage follow-up on llm.ResolveVision's target (the brain when that is false but the brain
// resolves) and swaps *cl to it; defer the returned func. A local vision model other than the brain's unloads the
// brain before and itself after, so one Ollama model stays resident (E26); a Claude-tier system prompt is re-rendered
// for the Ollama tier (V14). et-EE with a separate local vision model runs forkRelay instead.
func forkCameraReq(r *openai.ChatCompletionRequest, cl **openai.Client, esn string) func() {
	l, c, kg := lang.Current(), llm.Load(), forkKG()
	t, ok := forkResolve(c, l, kg)
	v, vok := llm.ResolveVision(c, l, kg)
	if l == lang.Estonian && vok && ok && v.Ollama() && t.Model != v.Model {
		return forkCameraRelay(r, cl, esn, t, v)
	}
	if !vok && ok {
		logger.Println("myvector llm: camera " + l + " has no vision target — asking the brain " + t.Model)
		v, vok = t, true
	}
	if !vok {
		ollama := llm.IsOllama(kg)
		forkReplyCap(r, ollama, false, forkBrain(ollama, l))
		if kg.Provider == "openai" && kg.Model != "" {
			r.Model = kg.Model // upstream DoGetImage hardcodes GPT4oMini for openai
		}
		if ollama {
			r.ReasoningEffort = "none"
		}
		llm.SamplingFor("", r.Model).Apply(r)
		return func() {}
	}
	logger.Println("myvector llm: camera " + l + " → " + v.Provider + " " + v.Model + " @ " + v.BaseURL)
	*cl = llm.NewClient(v)
	r.Model = v.Model
	forkReplyCap(r, v.Ollama(), false, forkBrain(v.Ollama(), l))
	if v.Ollama() {
		r.ReasoningEffort = "none"
	}
	llm.SamplingFor(v.Provider, v.Model).Apply(r)
	brain, bok := t, ok
	if !ok && llm.IsOllama(kg) {
		brain, bok = llm.Target{Provider: llm.Ollama, BaseURL: kg.Endpoint, Model: kg.Model}, true
	}
	if v.Ollama() { // also an Ollama brain's own tier: a photo answer's prompt (PhotoCommands)
		forkOllamaTier(r, esn, l)
	}
	if !v.Ollama() || (bok && brain.Ollama() && brain.Model == v.Model) { // same model: unloading it only forces a cold reload
		return func() {}
	}
	if bok && brain.Ollama() {
		forkUnload(brain.OllamaBase(), brain.Model)
	}
	return func() { forkUnload(v.OllamaBase(), v.Model) }
}

// forkOllamaTier re-renders r's system message for l's Ollama tier as a photo answer, without search results (V14).
func forkOllamaTier(r *openai.ChatCompletionRequest, esn, l string) {
	brain := prompt.OllamaFor(l)
	if len(r.Messages) == 0 || r.Messages[0].Role != openai.ChatMessageRoleSystem {
		return
	}
	last := ""
	for _, m := range r.Messages[1:] {
		if m.Role == openai.ChatMessageRoleUser && m.Content != "" {
			last = m.Content
		}
	}
	msgs := append([]openai.ChatCompletionMessage{}, r.Messages...) // the caller's msgs keep the brain's prompt
	msgs[0].Content = forkSystem(esn, last, l, brain, "", true)
	r.Messages = msgs
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
