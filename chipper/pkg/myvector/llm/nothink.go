package llm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/sashabaranov/go-openai"
)

// NewClientNoThink is NewClient with Claude's thinking off: claude-haiku-5-5 thinks by default and
// reasoning_effort does not stop it (llm-trials.md (docs/)).
func NewClientNoThink(t Target) *openai.Client {
	cfg := openai.DefaultConfig(t.Key)
	cfg.BaseURL = t.BaseURL
	if !t.Ollama() {
		cfg.HTTPClient = &http.Client{Transport: noThinking{}}
	}
	return openai.NewClientWithConfig(cfg)
}

// noThinking adds `"thinking":{"type":"disabled"}` to the body (go-openai has no field for it).
type noThinking struct{}

func (noThinking) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		m["thinking"] = map[string]string{"type": "disabled"}
		if b, err = json.Marshal(m); err != nil {
			return nil, err
		}
	}
	r = r.Clone(r.Context())
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	r.Body, _ = r.GetBody()
	r.ContentLength = int64(len(b))
	return http.DefaultTransport.RoundTrip(r)
}
