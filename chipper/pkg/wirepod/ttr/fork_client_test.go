package wirepod_ttr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

const testClaudeKey = "sk-ant-test-0123456789-WxYz"

// fakeLLM records one chat request and answers "OK".
type fakeLLM struct {
	mu               sync.Mutex
	auth, path, body string
	unloads          []string
	*httptest.Server
}

func newFakeLLM(t *testing.T) *fakeLLM {
	f := &fakeLLM{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/api/generate" {
			var u struct{ Model string }
			json.Unmarshal(b, &u)
			f.unloads = append(f.unloads, u.Model)
			return
		}
		f.auth, f.path, f.body = r.Header.Get("Authorization"), r.URL.Path, string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"}}]}`)
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
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, TopP: 1}
	done := forkCameraReq(&r, new(*openai.Client))
	if r.Model != llm.DefaultENModel || r.ReasoningEffort != "none" || strings.Join(f.unloads, ",") != "estllm:x" {
		t.Errorf("before photo: model %q reasoning %q unloads %v", r.Model, r.ReasoningEffort, f.unloads)
	}
	if s := llm.SamplingFor(llm.Ollama, llm.DefaultENModel); r.TopP != s.TopP || r.PresencePenalty != s.PresencePenalty { // the vision model's sampling, not estllm's
		t.Errorf("before photo: top_p %v presence %v, want %v %v", r.TopP, r.PresencePenalty, s.TopP, s.PresencePenalty)
	}
	done()
	if strings.Join(f.unloads, ",") != "estllm:x,"+llm.DefaultENModel {
		t.Errorf("after photo: unloads %v", f.unloads)
	}
}

func TestForkCameraETSameModelNoUnload(t *testing.T) {
	f := newFakeLLM(t)
	useKnowledge(t, "openai", "", "gpt-4o-mini")
	saveLLM(t, lang.Estonian, llm.Config{OllamaURL: f.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{
		lang.Estonian: {Provider: llm.Ollama, Model: llm.DefaultENModel}, lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}})
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, TopP: 1}
	done := forkCameraReq(&r, new(*openai.Client))
	done()
	if r.Model != llm.DefaultENModel || len(f.unloads) != 0 { // et brain = vision model: no unload, no cold reload
		t.Errorf("model %q unloads %v, want %q and none", r.Model, f.unloads, llm.DefaultENModel)
	}
}

func TestForkCameraClaudeDropsTopP(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	saveLLM(t, lang.English, llm.Config{ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Claude, Model: "claude-haiku-5-5"}}})
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", TopP: 1}
	forkCameraReq(&r, new(*openai.Client))()
	if r.Model != "claude-haiku-5-5" || r.TopP != 0 || r.ReasoningEffort != "" {
		t.Errorf("camera request %q top_p %v reasoning %q", r.Model, r.TopP, r.ReasoningEffort)
	}
}

// The provider, not the model ID, drops top_p: an alias without the "claude" prefix must not send it.
func TestForkClaudeAliasDropsTopP(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	saveLLM(t, lang.English, llm.Config{ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: {Provider: llm.Claude, Model: "haiku-latest"}}})
	if r := CreateAIReq("hi", "esn", false, true); r.TopP != 0 {
		t.Errorf("KG top_p %v", r.TopP)
	}
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", TopP: 1}
	forkCameraReq(&r, new(*openai.Client))()
	if r.TopP != 0 {
		t.Errorf("camera top_p %v", r.TopP)
	}
}

func TestForkCameraLegacyOpenAIModel(t *testing.T) {
	useKnowledge(t, "openai", "", "gpt-4.1")
	lang.UseForTest(t, lang.English)
	r := openai.ChatCompletionRequest{Model: openai.GPT4oMini}
	forkCameraReq(&r, new(*openai.Client))()
	if r.Model != "gpt-4.1" {
		t.Errorf("camera model %q", r.Model)
	}
}

func TestForkCameraLegacyOllamaSampling(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	lang.UseForTest(t, lang.English)
	r := openai.ChatCompletionRequest{Model: "qwen3.5:4b", Temperature: 1, TopP: 1}
	forkCameraReq(&r, new(*openai.Client))()
	if r.Temperature != 0.7 || r.TopP != 0.8 || r.PresencePenalty != 1.5 {
		t.Errorf("legacy camera sampling %v %v %v", r.Temperature, r.TopP, r.PresencePenalty)
	}
}

// The camera chat goes through the client forkCameraReq swaps in (listening: on Ollama, as forkResolve); false
// (legacy) keeps the Knowledge client. Claude has its own fake, so a Claude swap is told apart from no swap.
func TestForkCameraClient(t *testing.T) {
	ollama, claude := llm.Profile{Provider: llm.Ollama, Model: "gemma4:e4b"}, llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	const vision, knowledge, claudeHit = "vision", "knowledge", "claude"
	for _, w := range []struct {
		name, l, model, hit string // hit: the fake that must get the chat
		en, et              *llm.Profile
		listening, ollamaKG bool // ollamaKG: Knowledge is "custom" at the vision fake's Ollama endpoint, model "llama:k"
	}{
		{"et ollama", lang.Estonian, "gemma4:e4b", vision, &ollama, &llm.Profile{Provider: llm.Ollama, Model: "estllm:x"}, false, false},
		{"legacy en", lang.English, "gpt-4.1", knowledge, nil, nil, false, false},
		{"et claude listening", lang.Estonian, "gemma4:e4b", vision, &ollama, &claude, true, false},
		{"en claude listening", lang.English, listen.Model, vision, &claude, &claude, true, false},
		{"legacy et listening", lang.Estonian, "llama:k", vision, nil, nil, true, true},
		{"legacy en listening", lang.English, listen.Model, vision, nil, nil, true, true},
		{"en claude", lang.English, "claude-haiku-5-5", claudeHit, &claude, &ollama, false, false},
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
			saveLLM(t, w.l, llm.Config{OllamaURL: fakes[vision].URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.English: *w.en, lang.Estonian: *w.et}})
		}
		freshListening(t)
		if w.listening {
			forkListening.Activate("00e20100", forkNow())
		}
		cfg := openai.DefaultConfig("")
		cfg.BaseURL = fakes[knowledge].URL + "/v1"
		c := openai.NewClientWithConfig(cfg)
		r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "mida sa näed?"}}}
		done := forkCameraReq(&r, &c)
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
