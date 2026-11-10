package wirepod_ttr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/relay"
	"github.com/sashabaranov/go-openai"
)

const forkRelayAsk = "What do you see?"

var forkRelayTimeout = 60 * time.Second // per hop (R3)

// forkRelay is the et-EE camera relay (2026-11-10-camera-et-relay.md (docs/plans/) R2–R9, R11) as the transport of the
// photo client: hop 0 brain (question → English), hop 1 vision (English answer), hop 2 brain (→ Estonian), answered
// to DoGetImage as one SSE chunk.
type forkRelay struct{ brain, vision llm.Target }

// forkRelayClient is a client for vision whose every request runs the relay.
func forkRelayClient(brain, vision llm.Target) *openai.Client {
	cfg := openai.DefaultConfig(vision.Key)
	cfg.BaseURL = vision.BaseURL
	cfg.HTTPClient = &http.Client{Transport: &forkRelay{brain, vision}}
	return openai.NewClientWithConfig(cfg)
}

// forkCameraRelay readies r as hop 1 (English Ollama tier, R4) on a forkRelay client; the relay unloads, so the
// returned func is a no-op (R7).
func forkCameraRelay(r *openai.ChatCompletionRequest, cl **openai.Client, esn string, brain, vision llm.Target) func() {
	logger.Println("myvector llm: camera " + lang.Estonian + " relay → " + vision.Model + " @ " + vision.BaseURL + ", brain " + brain.Provider + " " + brain.Model)
	*cl = forkRelayClient(brain, vision)
	r.Model, r.ReasoningEffort = vision.Model, "none"
	forkReplyCap(r, true, false, prompt.Ollama)
	llm.SamplingFor(vision.Provider, vision.Model).Apply(r)
	forkOllamaTier(r, esn, lang.English)
	return func() {}
}

func (f *forkRelay) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, f.fail(1, errors.New("no request body"))
	}
	raw, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, f.fail(1, err)
	}
	msgs, _ := body["messages"].([]any)
	tr := relay.NewTrace(time.Now())
	if tr != nil {
		logger.Println("myvector llm: camera relay trace → " + tr.Dir)
	}
	q := forkRelayQuestion(msgs)
	tr.Note("question (last user message before the photo)", q)
	ask, d0 := f.hop0(req.Context(), tr, q)
	if f.brain.Ollama() {
		forkUnload(f.brain.OllamaBase(), f.brain.Model)
	}
	hop1, cut1, d1, err := f.hop1(req, tr, body, msgs, ask)
	tr.Reply("hop 1 vision", f.vision.Model, d1, hop1, cut1, err)
	forkUnload(f.vision.OllamaBase(), f.vision.Model)
	if err != nil {
		return nil, f.fail(1, err)
	}
	reply, cut2, d2 := hop1, false, time.Duration(0)
	if _, text := relay.Split(hop1); text != "" {
		t2 := time.Now()
		var hop2 string
		hop2, cut2, err = f.chat(req.Context(), tr, "hop 2 to Estonian", relay.Translate(f.brain, hop1))
		d2 = time.Since(t2)
		if err == nil && strings.TrimSpace(hop2) == "" {
			err = errors.New("empty reply")
		}
		if err != nil {
			tr.Note("hop 2 failed", err.Error())
			return nil, f.fail(2, err)
		}
		reply = relay.Attach(hop1, hop2)
	}
	tr.Note("final reply (to DoGetImage, spoken)", reply)
	logger.Println(fmt.Sprintf("myvector llm: camera relay %q %s %dms → %s %dms %q → %s %dms",
		ask, f.brain.Model, d0.Milliseconds(), f.vision.Model, d1.Milliseconds(), hop1, f.brain.Model, d2.Milliseconds()))
	return forkRelaySSE(req, f.vision.Model, reply, cut1 || cut2), nil
}

func (f *forkRelay) fail(hop int, err error) error {
	logger.Println("myvector llm: camera relay failed: hop" + strconv.Itoa(hop) + ": " + err.Error())
	return fmt.Errorf("camera relay hop %d failed", hop) // fixed text: DoGetImage retries on a "does not exist" error
}

// hop0 is q in English on the still-loaded brain, or forkRelayAsk when q is empty or the hop fails (R4, R11).
func (f *forkRelay) hop0(ctx context.Context, tr *relay.Trace, q string) (string, time.Duration) {
	if strings.TrimSpace(q) == "" {
		tr.Note("hop 0 skipped (no question)", "vision ask = "+forkRelayAsk)
		return forkRelayAsk, 0
	}
	t0 := time.Now()
	en, _, err := f.chat(ctx, tr, "hop 0 to English", relay.ToEnglish(f.brain, q))
	d := time.Since(t0)
	if en = strings.TrimSpace(en); err != nil || en == "" {
		if err == nil {
			err = errors.New("empty reply")
		}
		f.fail(0, err)
		tr.Note("hop 0 failed", "vision ask = "+forkRelayAsk)
		return forkRelayAsk, d
	}
	return en, d
}

// hop1 forwards the photo request to the vision model unstreamed, ask as the photo message's first text part (R3, R4);
// its duration is the HTTP call's only.
func (f *forkRelay) hop1(req *http.Request, tr *relay.Trace, body map[string]any, msgs []any, ask string) (string, bool, time.Duration, error) {
	if len(msgs) > 0 {
		if last, ok := msgs[len(msgs)-1].(map[string]any); ok {
			if parts, ok := last["content"].([]any); ok {
				last["content"] = append([]any{map[string]any{"type": "text", "text": ask}}, parts...)
			}
		}
	}
	body["stream"] = false
	delete(body, "stream_options")
	tr.Note("hop 1 vision endpoint", req.URL.String())
	tr.Request("hop 1 vision", body)
	b, err := json.Marshal(body)
	if err != nil {
		return "", false, 0, err
	}
	t := time.Now()
	text, cut, err := f.post(req, b)
	return text, cut, time.Since(t), err
}

func (f *forkRelay) post(req *http.Request, b []byte) (string, bool, error) {
	ctx, cancel := context.WithTimeout(req.Context(), forkRelayTimeout)
	defer cancel()
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL.String(), bytes.NewReader(b))
	if err != nil {
		return "", false, err
	}
	out.Header = req.Header.Clone()
	resp, err := http.DefaultClient.Do(out)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", false, errors.New(resp.Status + ": " + strings.TrimSpace(string(msg)))
	}
	var r openai.ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", false, err
	}
	if len(r.Choices) == 0 || strings.TrimSpace(r.Choices[0].Message.Content) == "" {
		return "", false, errors.New("empty reply")
	}
	return r.Choices[0].Message.Content, r.Choices[0].FinishReason == openai.FinishReasonLength, nil
}

// chat is one brain hop (R5): r with the brain's sampling, forkRelayTimeout; request and reply traced as step.
func (f *forkRelay) chat(ctx context.Context, tr *relay.Trace, step string, r openai.ChatCompletionRequest) (string, bool, error) {
	llm.SamplingFor(f.brain.Provider, f.brain.Model).Apply(&r)
	tr.Request(step, r)
	t := time.Now()
	text, cut, err := f.complete(ctx, r)
	tr.Reply(step, f.brain.Model, time.Since(t), text, cut, err)
	return text, cut, err
}

func (f *forkRelay) complete(ctx context.Context, r openai.ChatCompletionRequest) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, forkRelayTimeout)
	defer cancel()
	resp, err := llm.NewClient(f.brain).CreateChatCompletion(ctx, r)
	if err != nil {
		return "", false, err
	}
	if len(resp.Choices) == 0 {
		return "", false, nil
	}
	return resp.Choices[0].Message.Content, resp.Choices[0].FinishReason == openai.FinishReasonLength, nil
}

// forkRelayQuestion is the last user message's text before the photo message (R11).
func forkRelayQuestion(msgs []any) string {
	for i := len(msgs) - 2; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if s, ok := m["content"].(string); ok && m["role"] == openai.ChatMessageRoleUser && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// forkRelaySSE is reply as one stream chunk + [DONE]; finish_reason length when a hop hit its cap (R6).
func forkRelaySSE(req *http.Request, model, reply string, cut bool) *http.Response {
	finish := openai.FinishReasonStop
	if cut {
		finish = openai.FinishReasonLength
	}
	chunk, _ := json.Marshal(openai.ChatCompletionStreamResponse{ID: "relay", Object: "chat.completion.chunk", Model: model,
		Choices: []openai.ChatCompletionStreamChoice{{Delta: openai.ChatCompletionStreamChoiceDelta{Role: openai.ChatMessageRoleAssistant, Content: reply}, FinishReason: finish}}})
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Request: req,
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body:   io.NopCloser(strings.NewReader("data: " + string(chunk) + "\n\ndata: [DONE]\n\n"))}
}
