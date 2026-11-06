package wirepod_ttr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func useKnowledge(t *testing.T, provider, endpoint, model string) {
	t.Helper()
	old := vars.APIConfig.Knowledge
	t.Cleanup(func() { vars.APIConfig.Knowledge = old })
	vars.APIConfig.Knowledge.Provider = provider
	vars.APIConfig.Knowledge.Endpoint = endpoint
	vars.APIConfig.Knowledge.Model = model
	vars.APIConfig.Knowledge.OpenAIPrompt = ""
	vars.APIConfig.Knowledge.CommandsEnable = false
	vars.APIConfig.Knowledge.SaveChat = false
}

const ollamaEndpoint = "http://localhost:11434/v1"

func TestForkPromptEstonian(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	r := CreateAIReq("mis kell on?", "esn", false, true)
	if r.Model != lang.DefaultETModel {
		t.Errorf("Model = %q, want %q", r.Model, lang.DefaultETModel)
	}
	if !strings.HasSuffix(r.Messages[0].Content, "\n\nAnswer in Estonian.") {
		t.Errorf("system prompt lacks the language line: %q", r.Messages[0].Content)
	}
	if r.ReasoningEffort != "none" {
		t.Errorf("ReasoningEffort = %q", r.ReasoningEffort)
	}
}

func TestForkPromptEnglishUnchanged(t *testing.T) {
	lang.UseForTest(t, lang.English)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	r := CreateAIReq("what time is it", "esn", false, true)
	want := CreatePrompt("You are a helpful, animated robot called Vector. Keep the response concise yet informative.", "qwen3.5:4b", true)
	if r.Model != "qwen3.5:4b" || r.Messages[0].Content != want {
		t.Errorf("English request changed: model %q, prompt %q", r.Model, r.Messages[0].Content)
	}
	if r.ReasoningEffort != "none" {
		t.Errorf("Ollama ReasoningEffort = %q, want none", r.ReasoningEffort)
	}
}

// Claude and Gemini presets are "custom" too (scripts/llm.sh): only the Ollama port gets the ET model + reasoning.
func TestForkPromptOpenAIOmitsReasoning(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	for _, k := range [][3]string{
		{"openai", "", "gpt-4o-mini"},
		{"custom", "https://api.anthropic.com/v1", "claude-haiku-5-5"},
		{"custom", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-flash"},
	} {
		useKnowledge(t, k[0], k[1], k[2])
		r := CreateAIReq("mis kell on?", "esn", false, true)
		if r.ReasoningEffort != "" || r.Model != k[2] {
			t.Errorf("%s %s: ReasoningEffort %q, Model %q", k[0], k[1], r.ReasoningEffort, r.Model)
		}
		if !strings.HasSuffix(r.Messages[0].Content, forkETLine) {
			t.Errorf("%s: language line missing", k[1])
		}
	}
}

func TestForkCameraEstonianUnloads(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model     string `json:"model"`
			KeepAlive *int   `json:"keep_alive"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/api/generate" || b.KeepAlive == nil || *b.KeepAlive != 0 {
			t.Errorf("unload request %s %+v", r.URL.Path, b)
		}
		mu.Lock()
		got = append(got, b.Model)
		mu.Unlock()
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	old := forkOllamaPort
	forkOllamaPort = u.Port()
	t.Cleanup(func() { forkOllamaPort = old })
	useKnowledge(t, "custom", srv.URL+"/v1", "qwen3.5:4b")

	for _, c := range []struct {
		lang   string
		before []string
		after  []string
	}{
		{lang.English, nil, nil},
		{lang.Estonian, []string{lang.DefaultETModel}, []string{lang.DefaultETModel, "qwen3.5:4b"}},
	} {
		lang.UseForTest(t, c.lang)
		got = nil
		r := CreateAIReq("mida sa näed?", "esn", false, true)
		r.Model = vars.APIConfig.Knowledge.Model // DoGetImage's own model pick
		r.ReasoningEffort = ""
		done := forkCameraReq(&r)
		if r.ReasoningEffort != "none" || r.Model != "qwen3.5:4b" {
			t.Errorf("%s: camera request ReasoningEffort %q, Model %q", c.lang, r.ReasoningEffort, r.Model)
		}
		if strings.Join(got, ",") != strings.Join(c.before, ",") {
			t.Errorf("%s: unloads before photo = %v, want %v", c.lang, got, c.before)
		}
		done()
		if strings.Join(got, ",") != strings.Join(c.after, ",") {
			t.Errorf("%s: unloads after photo = %v, want %v", c.lang, got, c.after)
		}
	}
}
