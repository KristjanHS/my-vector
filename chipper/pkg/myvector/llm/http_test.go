package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

const testKey = "sk-ant-0123456789abcdefWxYz"

type fakeAPI struct {
	tags, models atomic.Int32
	tagsFail     atomic.Bool
	tagsSlow     chan struct{} // non-nil: /api/tags waits on it
	tagsBody     atomic.Value  // a string replacing the default /api/tags body
	ping         atomic.Value  // the last chat request body
	*httptest.Server
}

func newFakeAPI(t *testing.T, slow bool) *fakeAPI {
	f := &fakeAPI{}
	if slow {
		f.tagsSlow = make(chan struct{})
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			f.tags.Add(1)
			if f.tagsSlow != nil {
				<-f.tagsSlow
			}
			if f.tagsFail.Load() {
				http.Error(w, "down", 500)
				return
			}
			if b, ok := f.tagsBody.Load().(string); ok {
				io.WriteString(w, b)
				return
			}
			io.WriteString(w, `{"models":[{"name":"qwen3.5:4b"},{"name":"estllm:x"}]}`)
		case "/v1/models":
			f.models.Add(1)
			if r.Header.Get("x-api-key") != testKey || r.Header.Get("anthropic-version") != AnthropicVersion || r.URL.Query().Get("limit") != "100" {
				t.Errorf("models request headers %v query %q", r.Header, r.URL.RawQuery)
			}
			io.WriteString(w, `{"data":[{"id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5"}]}`)
		case "/v1/chat/completions":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), TestPrompt) {
				t.Errorf("ping body %s", b)
			}
			f.ping.Store(string(b))
			io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		if f.tagsSlow != nil {
			close(f.tagsSlow)
		}
		f.Close()
	})
	old := AnthropicBase
	AnthropicBase = f.URL
	t.Cleanup(func() { AnthropicBase = old })
	return f
}

func noKG() KG { return KG{} }

func getModels(t *testing.T, query string) (out struct {
	Ollama struct {
		Models []string
		Vision []string
		Error  string
	}
	Claude struct {
		Models []ClaudeModel
		Error  string
		KeySet bool
	}
}) {
	t.Helper()
	w := httptest.NewRecorder()
	ModelsHandler(noKG)(w, httptest.NewRequest(http.MethodGet, ModelsAPI+query, nil))
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("GET models: %d %s", w.Code, w.Body)
	}
	return out
}

func TestLLMModelsCache(t *testing.T) {
	tempPath(t)
	clearCache()
	f := newFakeAPI(t, false)
	if err := Save(Config{OllamaURL: f.URL, ClaudeKey: testKey}); err != nil {
		t.Fatal(err)
	}
	got := getModels(t, "")
	if strings.Join(got.Ollama.Models, ",") != "qwen3.5:4b,estllm:x" || len(got.Claude.Models) != 1 || got.Claude.Models[0] != (ClaudeModel{"claude-haiku-5-5", "Claude Haiku 5.5"}) || !got.Claude.KeySet {
		t.Fatalf("models %+v", got)
	}
	getModels(t, "")
	if f.tags.Load() != 1 || f.models.Load() != 1 {
		t.Fatalf("inside the TTL: tags %d models %d fetches, want 1", f.tags.Load(), f.models.Load())
	}
	getModels(t, "?refresh=1")
	if f.tags.Load() != 2 || f.models.Load() != 2 {
		t.Fatalf("refresh: tags %d models %d, want 2", f.tags.Load(), f.models.Load())
	}
	f.tagsFail.Store(true)
	if got := getModels(t, "?refresh=1"); got.Ollama.Error == "" || len(got.Ollama.Models) != 0 {
		t.Fatalf("failing tags: %+v", got.Ollama)
	}
	f.tagsFail.Store(false)
	getModels(t, "") // errors are not stored: the earlier success serves
	if n := f.tags.Load(); n != 3 {
		t.Fatalf("after an error: %d tag fetches, want the old success reused (3)", n)
	}
	if err := Save(Config{OllamaURL: f.URL}); err != nil { // a URL/key save clears the cache
		t.Fatal(err)
	}
	if got := getModels(t, ""); f.tags.Load() != 4 || got.Claude.KeySet || f.models.Load() != 3 || len(got.Claude.Models) != 0 {
		t.Fatalf("after Save without key: tags %d models %d, %+v", f.tags.Load(), f.models.Load(), got.Claude)
	}
}

func TestModelsVision(t *testing.T) {
	tempPath(t)
	clearCache()
	f := newFakeAPI(t, false)
	if err := Save(Config{OllamaURL: f.URL}); err != nil {
		t.Fatal(err)
	}
	f.tagsBody.Store(`{"models":[{"name":"qwen3.5:4b","capabilities":["completion","vision"]},{"name":"estllm:x","capabilities":["completion","tools"]},{"name":"gemma4:e4b","capabilities":["vision"]}]}`)
	if got := getModels(t, ""); strings.Join(got.Ollama.Models, ",") != "qwen3.5:4b,estllm:x,gemma4:e4b" || strings.Join(got.Ollama.Vision, ",") != "qwen3.5:4b" {
		t.Fatalf("with capabilities: %+v", got.Ollama)
	}
	f.tagsBody.Store(`{"models":[{"name":"qwen3.5:4b"},{"name":"estllm:x"}]}`)
	if got := getModels(t, "?refresh=1"); len(got.Ollama.Models) != 2 || got.Ollama.Vision == nil || len(got.Ollama.Vision) != 0 {
		t.Fatalf("no capabilities: %+v, want vision [] (not null)", got.Ollama)
	}
}

func TestLLMModelsTimeout(t *testing.T) {
	tempPath(t)
	clearCache()
	f := newFakeAPI(t, true)
	if err := Save(Config{OllamaURL: f.URL}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	got := getModels(t, "")
	if d := time.Since(start); got.Ollama.Error == "" || d > 3500*time.Millisecond {
		t.Fatalf("slow Ollama: error %q after %v", got.Ollama.Error, d)
	}
}

func postTest(body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, TestAPI, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	TestHandler(noKG)(w, r)
	return w
}

func TestLLMTestHandler(t *testing.T) {
	tempPath(t)
	f := newFakeAPI(t, false)
	if err := Save(Config{OllamaURL: f.URL}); err != nil {
		t.Fatal(err)
	}
	w := postTest(`{"lang":"et-EE","provider":"ollama","model":"estllm:x"}`)
	var got struct {
		Reply string
		Ms    int64
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Reply != "OK" {
		t.Fatalf("ollama test: %d %s", w.Code, w.Body)
	}
	if b := f.ping.Load().(string); !strings.Contains(b, `"max_tokens":32`) || strings.Contains(b, "max_completion_tokens") {
		t.Errorf("ollama ping cap: %s, want max_tokens only", b)
	}
	if w := postTest(`{"lang":"et-EE","provider":"claude","model":"claude-haiku-5-5"}`); w.Code != 400 {
		t.Fatalf("claude test without key: %d, want 400", w.Code)
	}
	if err := Save(Config{OllamaURL: f.URL, ClaudeKey: testKey}); err != nil {
		t.Fatal(err)
	}
	if w := postTest(`{"lang":"en-US","provider":"claude","model":"claude-haiku-5-5"}`); w.Code != 200 {
		t.Fatalf("claude test: %d %s", w.Code, w.Body)
	}
	if b := f.ping.Load().(string); !strings.Contains(b, `"max_completion_tokens":256`) || strings.Contains(b, `"max_tokens"`) {
		t.Errorf("claude ping cap: %s, want max_completion_tokens only", b)
	}
	if w := postTest(`{"lang":"xx","provider":"ollama","model":"m"}`); w.Code != 400 {
		t.Fatalf("bad lang: %d", w.Code)
	}
	if lang.Current() != lang.English {
		t.Fatal("test changed the language")
	}
}

func TestLLMMask(t *testing.T) {
	for k, want := range map[string]string{"": "", "short": "set", testKey: "sk-ant-…WxYz"} {
		if got := Mask(k); got != want {
			t.Errorf("Mask(%q) = %q, want %q", k, got, want)
		}
	}
}
