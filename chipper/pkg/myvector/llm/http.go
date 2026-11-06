package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/sashabaranov/go-openai"
)

const (
	ModelsAPI     = "/myvector/api/llm/models"
	TestAPI       = "/myvector/api/llm/test"
	TagsTimeout   = 3 * time.Second
	ModelsTimeout = 5 * time.Second
	ListCacheTTL  = 60 * time.Second
	ListMaxBody   = 1 << 20
	TestTimeout   = 60 * time.Second
	TestPrompt    = "Reply with the single word OK."
)

// Mask shows a key as first 7 + "…" + last 4; "set" when shorter than 16; "" when unset.
func Mask(k string) string {
	switch {
	case k == "":
		return ""
	case len(k) < 16:
		return "set"
	}
	return k[:7] + "…" + k[len(k)-4:]
}

type ClaudeModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ollamaList struct {
	Models []string `json:"models"`
	Error  string   `json:"error,omitempty"`
}

type claudeList struct {
	Models []ClaudeModel `json:"models"`
	Error  string        `json:"error,omitempty"`
	KeySet bool          `json:"keySet"`
}

type cacheEntry struct {
	at  time.Time
	val any
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func clearCache() {
	cacheMu.Lock()
	cache = map[string]cacheEntry{}
	cacheMu.Unlock()
}

// cached returns fetch's value for key, reusing a success younger than ListCacheTTL unless refresh.
func cached[T any](key string, refresh bool, fetch func() (T, error)) (T, error) {
	cacheMu.Lock()
	e, ok := cache[key]
	cacheMu.Unlock()
	if ok && !refresh && time.Since(e.at) < ListCacheTTL {
		return e.val.(T), nil
	}
	v, err := fetch()
	if err == nil {
		cacheMu.Lock()
		cache[key] = cacheEntry{time.Now(), v}
		cacheMu.Unlock()
	}
	return v, err
}

// getJSON GETs url into v within timeout; a non-200 is "HTTP <status>" (the body may echo request headers).
func getJSON(url string, timeout time.Duration, hdr map[string]string, v any) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, h := range hdr {
		req.Header.Set(k, h)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, ListMaxBody)).Decode(v)
}

func listOllama(base string) ([]string, error) {
	var b struct {
		Models []struct{ Name string } `json:"models"`
	}
	if err := getJSON(base+"/api/tags", TagsTimeout, nil, &b); err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	ms := []string{}
	for _, m := range b.Models {
		ms = append(ms, m.Name)
	}
	return ms, nil
}

func listClaude(key string) ([]ClaudeModel, error) {
	var b struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	hdr := map[string]string{"x-api-key": key, "anthropic-version": AnthropicVersion}
	if err := getJSON(AnthropicBase+"/v1/models?limit=100", ModelsTimeout, hdr, &b); err != nil {
		return nil, fmt.Errorf("claude: %w", err)
	}
	ms := []ClaudeModel{}
	for _, m := range b.Data {
		ms = append(ms, ClaudeModel{m.ID, m.DisplayName})
	}
	return ms, nil
}

func keyID(k string) string {
	s := sha256.Sum256([]byte(k))
	return hex.EncodeToString(s[:])[:8]
}

// ModelsHandler serves GET ModelsAPI[?refresh=1]: the Ollama and Claude model lists, fetched concurrently.
func ModelsHandler(kg func() KG) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "GET", http.StatusMethodNotAllowed)
			return
		}
		refresh := r.URL.Query().Get("refresh") == "1"
		c := Load()
		_, base := Effective(c, kg())
		var out struct {
			Ollama ollamaList `json:"ollama"`
			Claude claudeList `json:"claude"`
		}
		out.Ollama.Models, out.Claude.Models = []string{}, []ClaudeModel{}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms, err := cached("ollama:"+base, refresh, func() ([]string, error) { return listOllama(base) })
			if err != nil {
				out.Ollama.Error = err.Error()
				return
			}
			out.Ollama.Models = ms
		}()
		if out.Claude.KeySet = c.ClaudeKey != ""; out.Claude.KeySet {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ms, err := cached("claude:"+keyID(c.ClaudeKey), refresh, func() ([]ClaudeModel, error) { return listClaude(c.ClaudeKey) })
				if err != nil {
					out.Claude.Error = err.Error()
					return
				}
				out.Claude.Models = ms
			}()
		}
		wg.Wait()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

type testRequest struct {
	Lang     string `json:"lang"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// TestHandler serves POST TestAPI {lang, provider, model}: one TestPrompt to that model with the saved URL/key.
func TestHandler(kg func() KG) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var q testRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		c := Load()
		err := dec.Decode(&q)
		if err == nil && !lang.Valid(q.Lang) {
			err = fmt.Errorf("lang: one of %v", lang.Supported)
		}
		if err == nil {
			err = Validate(Config{ClaudeKey: c.ClaudeKey, Profiles: map[string]Profile{q.Lang: {q.Provider, q.Model}}})
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, base := Effective(c, kg())
		t := Target{Ollama, base + "/v1", "ollama", q.Model}
		if q.Provider == Claude {
			t = Target{Claude, AnthropicBase + "/v1", c.ClaudeKey, q.Model}
		}
		reply, ms, err := ping(r.Context(), t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"reply": reply, "ms": ms})
	}
}

func ping(ctx context.Context, t Target) (string, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, TestTimeout)
	defer cancel()
	req := openai.ChatCompletionRequest{Model: t.Model, MaxCompletionTokens: 32,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: TestPrompt}}}
	if t.Ollama() {
		req.ReasoningEffort = "none"
	}
	start := time.Now()
	resp, err := NewClient(t).CreateChatCompletion(ctx, req)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		return "", ms, fmt.Errorf("%s %s: %w", t.Provider, t.Model, err)
	}
	if len(resp.Choices) == 0 {
		return "", ms, errors.New(t.Provider + " " + t.Model + ": no reply")
	}
	reply := []rune(resp.Choices[0].Message.Content)
	if len(reply) > 200 {
		reply = reply[:200]
	}
	return string(reply), ms, nil
}
