package wirepod_ttr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

const testClaudeKey = "sk-ant-test-0123456789-WxYz"

// fakeLLM records the last chat request and answers "OK", or replies[model] in order.
type fakeLLM struct {
	mu               sync.Mutex
	auth, path, body string
	unloads          []string
	events, bodies   []string            // "chat <model>" / "unload <model>", every chat body, in order
	replies          map[string][]string // per model, popped one per chat
	cut              map[string]bool     // model answers finish_reason length
	down             map[string]int      // model answers this status
	status           int                 // non-zero: every chat answers with it
	*httptest.Server
}

func newFakeLLM(t *testing.T) *fakeLLM {
	f := &fakeLLM{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		var u struct{ Model string }
		json.Unmarshal(b, &u)
		if r.URL.Path == "/api/generate" {
			f.unloads = append(f.unloads, u.Model)
			f.events = append(f.events, "unload "+u.Model)
			return
		}
		f.auth, f.path, f.body = r.Header.Get("Authorization"), r.URL.Path, string(b)
		f.events, f.bodies = append(f.events, "chat "+u.Model), append(f.bodies, string(b))
		if st := f.down[u.Model]; f.status != 0 || st != 0 {
			if f.status != 0 {
				st = f.status
			}
			w.WriteHeader(st)
			return
		}
		content, finish := "OK", "stop"
		if q := f.replies[u.Model]; len(q) > 0 {
			content, f.replies[u.Model] = q[0], q[1:]
		}
		if f.cut[u.Model] {
			finish = "length"
		}
		c, _ := json.Marshal(content)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":`+string(c)+`},"finish_reason":"`+finish+`"}]}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func saveLLM(t *testing.T, l string, c llm.Config) {
	t.Helper()
	lang.UseForTest(t, l)
	if err := llm.Save(c); err != nil {
		t.Fatal(err)
	}
}

func useAnthropic(t *testing.T, url string) {
	old := llm.AnthropicBase
	llm.AnthropicBase = url
	t.Cleanup(func() { llm.AnthropicBase = old })
}

// ask sends the KG request StreamingKGSim would, non-streamed, through forkClient.
func ask(t *testing.T) {
	t.Helper()
	r := CreateAIReq("mis kell on?", "esn", false, true)
	r.Stream = false
	if _, err := forkClient(nil).CreateChatCompletion(context.Background(), r); err != nil {
		t.Fatal(err)
	}
}

func TestForkClientClaude(t *testing.T) {
	f := newFakeLLM(t)
	useAnthropic(t, f.URL)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	saveLLM(t, lang.Estonian, llm.Config{ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{
		lang.Estonian: {Provider: llm.Claude, Model: "claude-haiku-5-5"}, lang.English: {Provider: llm.Ollama, Model: "qwen3.5:4b"}}})
	ask(t)
	if f.auth != "Bearer "+testClaudeKey || f.path != "/v1/chat/completions" {
		t.Errorf("auth %q path %q", f.auth, f.path)
	}
	if strings.Contains(f.body, "top_p") || strings.Contains(f.body, "reasoning_effort") || !strings.Contains(f.body, `"model":"claude-haiku-5-5"`) {
		t.Errorf("body %s", f.body)
	}
}

func TestForkKeyNotLeaked(t *testing.T) {
	f := newFakeLLM(t)
	useAnthropic(t, f.URL)
	useKnowledge(t, "openai", "", "gpt-4o-mini")
	vars.APIConfig.Knowledge.Key = "sk-openai-upstream"
	saveLLM(t, lang.English, llm.Config{ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}})
	ask(t)
	if vars.APIConfig.Knowledge.Key != "sk-openai-upstream" || f.auth != "Bearer "+testClaudeKey {
		t.Errorf("Knowledge.Key %q, auth %q", vars.APIConfig.Knowledge.Key, f.auth)
	}
}

func TestForkClientOllamaURL(t *testing.T) {
	f := newFakeLLM(t)
	useKnowledge(t, "custom", "http://192.0.2.1:11434/v1", "qwen3.5:4b") // unreachable: the saved URL must win
	saveLLM(t, lang.English, llm.Config{OllamaURL: f.URL + "/v1/", Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Ollama, Model: "gemma4:e4b"}}})
	ask(t)
	if f.path != "/v1/chat/completions" || !strings.Contains(f.body, `"model":"gemma4:e4b"`) || !strings.Contains(f.body, `"reasoning_effort":"none"`) {
		t.Errorf("path %q body %s", f.path, f.body)
	}
}

// llm.SamplingFor reaches the wire: each family's model-card sampling, whatever upstream CreateAIReq set.
func TestForkClientModelSampling(t *testing.T) {
	for model, want := range map[string][]string{
		"qwen3.5:4b":        {`"temperature":0.7`, `"top_p":0.8`, `"presence_penalty":1.5`},
		"gemma4:e4b-it-qat": {`"temperature":1`, `"top_p":0.95`},
	} {
		f := newFakeLLM(t)
		useKnowledge(t, "custom", "http://192.0.2.1:11434/v1", "qwen3.5:4b")
		saveLLM(t, lang.English, llm.Config{OllamaURL: f.URL, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Ollama, Model: model}}})
		ask(t)
		for _, w := range want {
			if !strings.Contains(f.body, w) {
				t.Errorf("%s: no %s in %s", model, w, f.body)
			}
		}
		if model == "gemma4:e4b-it-qat" && strings.Contains(f.body, "presence_penalty") {
			t.Errorf("gemma4 sent presence_penalty: %s", f.body)
		}
	}
}

func TestForkCameraETUnloadsAtProfileURL(t *testing.T) {
	f := newFakeLLM(t)
	useKnowledge(t, "openai", "", "gpt-4o-mini")
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: f.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{
		lang.Estonian: {Provider: llm.Ollama, Model: "estllm:x"}, lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}})
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, TopP: 1, Messages: forkPhotoMsgs("mida sa näed?")}
	c := openai.NewClient("")
	done := forkCameraReq(&r, &c, "esn")
	if r.Model != llm.DefaultVisionET || r.ReasoningEffort != "none" || len(f.unloads) != 0 {
		t.Errorf("before photo: model %q reasoning %q unloads %v", r.Model, r.ReasoningEffort, f.unloads)
	}
	if s := llm.SamplingFor(llm.Ollama, llm.DefaultVisionET); r.TopP != s.TopP || r.PresencePenalty != s.PresencePenalty { // the vision model's sampling, not estllm's
		t.Errorf("before photo: top_p %v presence %v, want %v %v", r.TopP, r.PresencePenalty, s.TopP, s.PresencePenalty)
	}
	if _, _, err := forkPhotoStream(c, r); err != nil {
		t.Fatal(err)
	}
	done()
	if strings.Join(f.unloads, ",") != "estllm:x,"+llm.DefaultVisionET {
		t.Errorf("after photo: unloads %v", f.unloads)
	}
}

func TestForkCameraETSameModelNoUnload(t *testing.T) {
	f := newFakeLLM(t)
	useKnowledge(t, "openai", "", "gpt-4o-mini")
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: f.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{
		lang.Estonian: {Provider: llm.Ollama, Model: llm.DefaultVisionET}, lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}})
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, TopP: 1}
	done := forkCameraReq(&r, new(*openai.Client), "esn")
	done()
	if r.Model != llm.DefaultVisionET || len(f.unloads) != 0 { // et brain = vision model: no unload, no cold reload
		t.Errorf("model %q unloads %v, want %q and none", r.Model, f.unloads, llm.DefaultVisionET)
	}
}

func TestForkCameraClaudeDropsTopP(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	claude := map[string]llm.Profile{lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}
	saveLLM(t, lang.English, llm.Config{ClaudeKey: testClaudeKey, Profiles: claude, Vision: claude})
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", TopP: 1}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if r.Model != "claude-haiku-5-5" || r.TopP != 0 || r.ReasoningEffort != "" {
		t.Errorf("camera request %q top_p %v reasoning %q", r.Model, r.TopP, r.ReasoningEffort)
	}
}

// The provider, not the model ID, drops top_p: an alias without the "claude" prefix must not send it.
func TestForkClaudeAliasDropsTopP(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	alias := map[string]llm.Profile{lang.English: {Provider: llm.Claude, Model: "haiku-latest"}}
	saveLLM(t, lang.English, llm.Config{ClaudeKey: testClaudeKey, Profiles: alias, Vision: alias})
	if r := CreateAIReq("hi", "esn", false, true); r.TopP != 0 {
		t.Errorf("KG top_p %v", r.TopP)
	}
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", TopP: 1}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if r.TopP != 0 {
		t.Errorf("camera top_p %v", r.TopP)
	}
}

func TestForkCameraLegacyOpenAIModel(t *testing.T) {
	useKnowledge(t, "openai", "", "gpt-4.1")
	lang.UseForTest(t, lang.English)
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if r.Model != "gpt-4.1" {
		t.Errorf("camera model %q", r.Model)
	}
}

// fakeOllamaKG makes Knowledge a "custom" Ollama endpoint on a fake, so no camera unload reaches the real Ollama.
func fakeOllamaKG(t *testing.T, model string) *fakeLLM {
	f := newFakeLLM(t)
	old := llm.OllamaPort
	llm.OllamaPort = f.URL[strings.LastIndex(f.URL, ":")+1:]
	t.Cleanup(func() { llm.OllamaPort = old })
	useKnowledge(t, "custom", f.URL+"/v1", model)
	return f
}

// DoGetImage (upstream) must keep its two V8/V15 hooks: the tests above call them directly.
func TestForkDoGetImageHooks(t *testing.T) {
	b, err := os.ReadFile("kgsim_cmds.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	body := s[strings.Index(s, "func DoGetImage("):]
	body = body[:strings.Index(body, "\nfunc ")]
	for _, h := range []string{"logger.Println(\"LLM error: \" + err.Error())\n\t\t\tforkVisionDown(robot)", "forkPhotoActs(GetActionsFromString("} {
		if !strings.Contains(body, h) {
			t.Errorf("DoGetImage lacks %q", h)
		}
	}
}

func TestForkCameraLegacyOllamaSampling(t *testing.T) {
	fakeOllamaKG(t, "qwen3.5:4b")
	lang.UseForTest(t, lang.English)
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", Temperature: 1, TopP: 1}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if r.Temperature != 0.7 || r.TopP != 0.8 || r.PresencePenalty != 1.5 {
		t.Errorf("legacy camera sampling %v %v %v", r.Temperature, r.TopP, r.PresencePenalty)
	}
}

// The camera chat goes through the client forkCameraReq swaps in: llm.ResolveVision's target (unsaved = the local
// default); false (legacy) keeps the Knowledge client. Claude has its own fake, so a Claude swap is told apart from no swap.
// et-EE rows with a separate local vision model run the relay: TestForkCameraRelay.
func TestForkCameraClient(t *testing.T) {
	ollama, claude := llm.Profile{Provider: llm.Ollama, Model: "gemma4:e4b"}, llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	llava := llm.Profile{Provider: llm.Ollama, Model: "llava:7b"} // a saved camera model: gemma4 is llm.NoVision
	const vision, knowledge, claudeHit = "vision", "knowledge", "claude"
	for _, w := range []struct {
		name, l, model, hit string // hit: the fake that must get the chat
		en, et              *llm.Profile
		cam                 map[string]llm.Profile // saved "vision"
		listening, ollamaKG bool                   // ollamaKG: Knowledge is "custom" at the vision fake's Ollama endpoint, model "llama:k"
	}{
		{"legacy en", lang.English, "gpt-4.1", knowledge, nil, nil, nil, false, false},
		{"en claude listening", lang.English, llm.DefaultVisionEN, vision, &claude, &claude, nil, true, false},
		{"legacy en listening", lang.English, llm.DefaultVisionEN, vision, nil, nil, nil, true, true},
		{"en claude", lang.English, llm.DefaultVisionEN, vision, &claude, &ollama, nil, false, false},
		{"en claude, vision claude", lang.English, "claude-haiku-5-5", claudeHit, &claude, &ollama, map[string]llm.Profile{lang.English: claude}, false, false},
		{"en ollama, vision llava", lang.English, "llava:7b", vision, &ollama, &ollama, map[string]llm.Profile{lang.English: llava}, false, false},
	} {
		fakes := map[string]*fakeLLM{vision: newFakeLLM(t), knowledge: newFakeLLM(t), claudeHit: newFakeLLM(t)}
		useAnthropic(t, fakes[claudeHit].URL)
		useKnowledge(t, "openai", "", "gpt-4.1")
		if w.ollamaKG {
			old := llm.OllamaPort
			llm.OllamaPort = fakes[vision].URL[strings.LastIndex(fakes[vision].URL, ":")+1:]
			t.Cleanup(func() { llm.OllamaPort = old })
			useKnowledge(t, "custom", fakes[vision].URL+"/v1", "llama:k")
		}
		lang.UseForTest(t, w.l)
		if w.en != nil {
			saveLLM(t, w.l, llm.Config{OllamaURL: fakes[vision].URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: *w.en, lang.Estonian: *w.et}, Vision: w.cam})
		}
		freshListening(t)
		if w.listening {
			forkListening.Activate("00e20100", forkNow())
		}
		cfg := openai.DefaultConfig("")
		cfg.BaseURL = fakes[knowledge].URL + "/v1"
		c := openai.NewClientWithConfig(cfg)
		r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "mida sa näed?"}}}
		done := forkCameraReq(&r, &c, "esn")
		if _, err := c.CreateChatCompletion(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		done()
		for k, f := range fakes {
			if k == w.hit && (f.path != "/v1/chat/completions" || !strings.Contains(f.body, `"model":"`+w.model+`"`)) {
				t.Errorf("%s: %s fake got %q %s", w.name, k, f.path, f.body)
			}
			if k != w.hit && f.path != "" {
				t.Errorf("%s: %s fake got the chat, want %s", w.name, k, w.hit)
			}
		}
	}
}

// Ollama down (500): the stream fails on the local target, Claude is not asked, and forkVisionDown speaks the V8 line.
func TestForkCameraOllamaDown(t *testing.T) {
	local, cl := newFakeLLM(t), newFakeLLM(t)
	local.status = http.StatusInternalServerError
	useAnthropic(t, cl.URL)
	useKnowledge(t, "openai", "", "gpt-4.1")
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: local.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.Estonian: claude}})
	var said []string
	old := forkVisionSay
	forkVisionSay = func(s string, _ *vector.Vector) error { said = append(said, s); return nil }
	t.Cleanup(func() { forkVisionSay = old })
	c := openai.NewClient("")
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Stream: true, Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "mida sa näed?"}}}
	done := forkCameraReq(&r, &c, "esn")
	if _, err := c.CreateChatCompletionStream(context.Background(), r); err == nil {
		t.Fatal("stream on a 500 Ollama: no error")
	}
	forkVisionDown(nil)
	done()
	if local.path != "/v1/chat/completions" || cl.path != "" || strings.Join(said, "|") != "Ma ei näe praegu." {
		t.Errorf("local %q claude %q said %q", local.path, cl.path, said)
	}
}

// Brain Claude + vision local: an over-budget Claude-tier system message reaches Ollama re-rendered within its budget.
func TestForkCameraOllamaTier(t *testing.T) {
	local := newFakeLLM(t)
	useKnowledge(t, "openai", "", "gpt-4.1")
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	saveLLM(t, lang.English, llm.Config{OllamaURL: local.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: claude}})
	budget := prompt.Budget(prompt.Ollama)
	big := strings.Repeat("Vector likes long stories. ", 4*budget/5)
	if prompt.Estimate(big) <= budget {
		t.Fatalf("fixture %d tokens, not over %d", prompt.Estimate(big), budget)
	}
	msgs := []openai.ChatCompletionMessage{{Role: "system", Content: big}, {Role: "user", Content: "what do you see?"}}
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: msgs}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if n := prompt.Estimate(r.Messages[0].Content); n > budget || r.Messages[1].Content != "what do you see?" || msgs[0].Content != big {
		t.Errorf("system %d tokens (budget %d), user %q, caller's prompt kept %v", n, budget, r.Messages[1].Content, msgs[0].Content == big)
	}
	small := []openai.ChatCompletionMessage{{Role: "system", Content: "short"}, {Role: "user", Content: "hi"}}
	r = openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: small}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if !strings.Contains(r.Messages[0].Content, prompt.SpeechBlock(prompt.Ollama)) {
		t.Errorf("in-budget system prompt not re-rendered: %q", r.Messages[0].Content)
	}
}

// et-EE relay: an in-budget Claude-tier prompt reaches the local vision model as the English Ollama tier (R4).
func TestForkCameraOllamaTierEt(t *testing.T) {
	local := newFakeLLM(t)
	useKnowledge(t, "openai", "", "gpt-4.1")
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: local.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.Estonian: claude}})
	msgs := []openai.ChatCompletionMessage{{Role: "system", Content: "short"}, {Role: "user", Content: "mida sa näed?"}}
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: msgs}
	forkCameraReq(&r, new(*openai.Client), "esn")()
	if !strings.Contains(r.Messages[0].Content, prompt.SpeechBlock(prompt.Ollama)) || strings.Contains(r.Messages[0].Content, prompt.LangLine(prompt.OllamaEt, lang.Estonian)) {
		t.Errorf("system prompt is not the English Ollama tier: %q", r.Messages[0].Content)
	}
	if msgs[0].Content != "short" {
		t.Errorf("caller's prompt changed: %q", msgs[0].Content)
	}
}

// V7: one Ollama model resident — brain = vision → none; brain gemma, vision llava → gemma before, llava after;
// brain Claude → vision after only. llava is no default, so a ResolveVision ignoring the saved profile fails.
func TestForkCameraUnload(t *testing.T) {
	llava, gemma := llm.Profile{Provider: llm.Ollama, Model: "llava:7b"}, llm.Profile{Provider: llm.Ollama, Model: "gemma4:e4b"}
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	for _, w := range []struct {
		name          string
		brain, vision llm.Profile
		before, after string
	}{
		{"same", llava, llava, "", ""},
		{"gemma brain, llava vision", gemma, llava, "gemma4:e4b", "gemma4:e4b,llava:7b"},
		{"claude brain", claude, llava, "", "llava:7b"},
	} {
		f := newFakeLLM(t)
		useKnowledge(t, "openai", "", "gpt-4.1")
		saveLLM(t, lang.English, llm.Config{OllamaURL: f.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: w.brain},
			Vision: map[string]llm.Profile{lang.English: w.vision}})
		r := openai.ChatCompletionRequest{Model: openai.GPT4oMini}
		done := forkCameraReq(&r, new(*openai.Client), "esn")
		before := strings.Join(f.unloads, ",")
		done()
		if before != w.before || strings.Join(f.unloads, ",") != w.after {
			t.Errorf("%s: unloads before %q after %q, want %q %q", w.name, before, strings.Join(f.unloads, ","), w.before, w.after)
		}
	}
}
