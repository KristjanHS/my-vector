package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

func useModel(t *testing.T, cfg string) *[]string {
	t.Helper()
	oldCfg, oldLog := configBytes, logf
	var logs []string
	configBytes = func() ([]byte, error) { return []byte(cfg), nil }
	logf = func(a ...any) { logs = append(logs, fmt.Sprint(a...)) }
	t.Cleanup(func() { configBytes, logf = oldCfg, oldLog })
	return &logs
}

func TestTarget(t *testing.T) {
	ollama := llm.DefaultOllamaURL + "/v1"
	for _, c := range []struct {
		name, cfg, key string
		want           llm.Target
		ok             bool
		log            string
	}{
		{"default", `{}`, "", llm.Target{Provider: llm.Ollama, BaseURL: ollama, Key: "ollama", Model: llm.DefaultETModel}, true, ""},
		{"bad file", `{`, "", llm.Target{Provider: llm.Ollama, BaseURL: ollama, Key: "ollama", Model: llm.DefaultETModel}, true, ""},
		{"ollama: alone", `{"memory_model":"ollama:"}`, "", llm.Target{Provider: llm.Ollama, BaseURL: ollama, Key: "ollama", Model: llm.DefaultETModel}, true, ""},
		{"ollama tag keeps its colon", `{"memory_model":"ollama:qwen3.5:9b"}`, "k", llm.Target{Provider: llm.Ollama, BaseURL: ollama, Key: "ollama", Model: "qwen3.5:9b"}, true, ""},
		{"claude no key", `{"memory_model":"claude:claude-haiku-5-5"}`, "", llm.Target{}, false, "no Claude key"},
		{"claude", `{"memory_model":"claude:claude-haiku-5-5"}`, "k", llm.Target{Provider: llm.Claude, BaseURL: llm.AnthropicBase + "/v1", Key: "k", Model: "claude-haiku-5-5"}, true, ""},
		{"off", `{"memory_model":"off"}`, "k", llm.Target{}, false, ""},
		{"unknown provider", `{"memory_model":"gpt:x"}`, "k", llm.Target{}, false, "unknown"},
		{"claude: alone", `{"memory_model":"claude:"}`, "k", llm.Target{}, false, "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			logs := useModel(t, c.cfg)
			got, ok := Target(llm.Config{ClaudeKey: c.key}, lang.Estonian, llm.KG{})
			if got != c.want || ok != c.ok {
				t.Fatalf("Target = %+v, %v; want %+v, %v", got, ok, c.want, c.ok)
			}
			if j := strings.Join(*logs, "\n"); (c.log == "") != (j == "") || !strings.Contains(j, c.log) {
				t.Fatalf("logs %q, want one containing %q", j, c.log)
			}
		})
	}
}

// fakeChat serves one chat completion (status 200 replies "ok"); hits counts requests, body keeps the last one.
func fakeChat(t *testing.T, status int) (url string, hits *int, body *map[string]any) {
	t.Helper()
	hits, body = new(int), &map[string]any{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, body)
		if status != http.StatusOK {
			http.Error(w, "boom", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":" ok "}}]}`)
	}))
	t.Cleanup(s.Close)
	return s.URL, hits, body
}

func TestTargetOllamaFailureNoClaude(t *testing.T) {
	useModel(t, `{}`)
	ollama, _, _ := fakeChat(t, http.StatusInternalServerError)
	claude, claudeHits, _ := fakeChat(t, http.StatusOK)
	old := llm.AnthropicBase
	llm.AnthropicBase = claude
	t.Cleanup(func() { llm.AnthropicBase = old })

	tg, ok := Target(llm.Config{Profiles: map[string]llm.Profile{}, OllamaURL: ollama, ClaudeKey: "k"}, lang.Estonian, llm.KG{})
	if !ok || !tg.Ollama() {
		t.Fatalf("Target = %+v, %v; want the Ollama default", tg, ok)
	}
	if _, err := Ask(context.Background(), tg, "sys", "user", 50); err == nil {
		t.Fatal("Ask on a 500 Ollama: want an error")
	}
	if *claudeHits != 0 {
		t.Fatalf("Claude got %d calls after an Ollama failure; want 0", *claudeHits)
	}
}

func TestAskRequest(t *testing.T) {
	useModel(t, `{}`)
	u, hits, body := fakeChat(t, http.StatusOK)
	got, err := Ask(context.Background(), llm.Target{Provider: llm.Ollama, BaseURL: u + "/v1", Key: "ollama", Model: llm.DefaultETModel}, "sys", "user", 50)
	if err != nil || got != "ok" || *hits != 1 {
		t.Fatalf("Ask = %q, %v, hits %d; want \"ok\", nil, 1", got, err, *hits)
	}
	b := *body
	if temp, ok := b["temperature"].(float64); !ok || temp <= 0 || temp > 1e-6 {
		t.Fatalf("temperature = %v; want sent and ~0", b["temperature"])
	}
	if b["reasoning_effort"] != "none" || b["max_tokens"] != float64(50) {
		t.Fatalf("reasoning_effort %v, max_tokens %v; want none, 50", b["reasoning_effort"], b["max_tokens"])
	}
}

func TestAskRefusesSwitchedModel(t *testing.T) {
	u, hits, _ := fakeChat(t, http.StatusOK)
	tg := llm.Target{Provider: llm.Ollama, BaseURL: u + "/v1", Key: "ollama", Model: llm.DefaultETModel}
	for _, cfg := range []string{`{"memory_model":"off"}`, `{"memory_model":"claude:` + llm.DefaultETModel + `"}`, `{"memory_model":"ollama:other"}`} {
		useModel(t, cfg)
		if _, err := Ask(context.Background(), tg, "sys", "user", 50); err == nil || *hits != 0 {
			t.Fatalf("%s: Ask err %v, %d calls; want refused, none", cfg, err, *hits)
		}
	}
	useModel(t, `{"memory_model":"ollama:"}`)
	if _, err := Ask(context.Background(), tg, "sys", "user", 50); err != nil || *hits != 1 {
		t.Fatalf("ollama: names the default model: err %v, %d calls", err, *hits)
	}
}
