package probekit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func TestStats(t *testing.T) {
	var calls []Call
	for i := 1; i <= 10; i++ {
		calls = append(calls, Call{Reply: "ok", Finish: "stop", Prompt: 100, Completion: 1, ReplyTok: 1, Counted: true, Dur: ms(i * 100)})
	}
	calls[0] = Call{Reply: "abcdefghi", Reasoning: "hmm", Finish: "stop", Prompt: 300, Completion: 50, ReplyTok: 3, Counted: true, Dur: ms(100)}
	calls[1].Finish = "length"
	calls[2].Reply = ""
	calls[3].Reasoning, calls[3].Completion = "x", 21  // 21 − 1 = 20
	calls[4].ReplyTok = 5                              // 1 − 5 floors at 0, not exact, err −4
	calls[5].Counted, calls[5].Completion = false, 90  // uncounted: in N and latency, not in T, exact or err
	calls[6] = Call{Finish: ErrorFinish, Dur: ms(700)} // a failed call, 0/0: a cut, uncounted
	s := Summarize(calls)
	want := Summary{N: 10, Counted: 8, P50: ms(500), P90: ms(900), CutPct: 30, ThinkPct: 20, ExactPct: 62.5, T25: 0, T50: 0, T90: 47,
		ErrMin: -4, ErrMax: 47, MaxPrompt: 300, MaxCtx: 350}
	if s != want {
		t.Errorf("Summarize = %+v; want %+v", s, want)
	}
	if l := s.Line("x"); !strings.Contains(l, "T 0/0/47  exact 62% (n 8)  err -4/+47") {
		t.Errorf("Line = %q", l)
	}
	if got := Summarize(calls[:1]); got.T50 != 47 || got.P50 != ms(100) {
		t.Errorf("one call: T50 %d P50 %v; want 47, 100ms", got.T50, got.P50)
	}
	if got := Summarize(calls[1:5]); got.ErrMin != -4 || got.ErrMax != 20 { // errs 0, 0, 20, −4: neither bound comes first
		t.Errorf("err = %d/%d; want -4/+20", got.ErrMin, got.ErrMax)
	}
	none := Summarize(calls[5:7])
	if none.Counted != 0 || none.ExactPct != 0 || none.T90 != 0 || none.N != 2 ||
		!strings.Contains(none.Line("x"), "T -  exact - (n 0)  err -") {
		t.Errorf("nothing counted = %+v, %q", none, none.Line("x"))
	}
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("no calls = %+v; want zero", got)
	}
}

// countServer answers every request with prompt_tokens = 9 + len(first message content) and records the requests.
func countServer(t *testing.T, sent *[]openai.ChatCompletionRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m struct{ Messages []map[string]any }
		_ = json.Unmarshal(raw, &m)
		for _, x := range m.Messages {
			if _, ok := x["content"]; !ok { // as Ollama: "invalid message content type: <nil>"
				http.Error(w, "no content", http.StatusBadRequest)
				return
			}
		}
		var q openai.ChatCompletionRequest
		_ = json.Unmarshal(raw, &q)
		*sent = append(*sent, q)
		n := 9
		if len(q.Messages) > 0 {
			n += len(q.Messages[0].Content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "x"},
			"finish_reason": "length"}}, "usage": map[string]int{"prompt_tokens": n, "completion_tokens": 1}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCountReply(t *testing.T) {
	var sent []openai.ChatCompletionRequest
	srv := countServer(t, &sent)
	model := uniqueModel()
	for _, c := range []struct {
		content, finish string
		want            int
	}{{"abc", "stop", 4}, {"abc", "length", 3}, {"", "stop", 1}, {"", "length", 0}} {
		if got, err := CountReply(srv.URL, model, c.content, c.finish); err != nil || got != c.want {
			t.Errorf("CountReply(%q, %q) = %d, %v; want %d", c.content, c.finish, got, err, c.want)
		}
	}
	if len(sent) != 3 { // "" baseline once (cached), "abc" twice, nothing for empty content
		t.Fatalf("requests = %d; want 3", len(sent))
	}
	if q := sent[0]; q.Model != model || q.MaxTokens != 1 || q.ReasoningEffort != "none" || q.Messages[0].Content != "" {
		t.Errorf("baseline request = %+v", q)
	}
	if _, err := CountReply(srv.URL+"/nope", model, "abc", "stop"); err == nil {
		t.Error("bad URL: want an error")
	}
}

var models int

// uniqueModel is a model name no earlier test (or -count run) used: CountReply caches the baseline per baseURL + model.
func uniqueModel() string {
	models++
	return fmt.Sprintf("m%d", models)
}

func TestCountAll(t *testing.T) {
	var sent []openai.ChatCompletionRequest
	srv := countServer(t, &sent)
	calls := []Call{{Raw: " ab", Finish: "stop", Completion: 4}, {Finish: ErrorFinish}}
	got, err := CountAll(srv.URL, uniqueModel(), calls)
	if err != nil || !got[0].Counted || got[0].ReplyTok != 4 || got[1].Counted || len(sent) != 2 {
		t.Errorf("CountAll = %+v, %v, %d requests; want ReplyTok 4 counted, error call skipped, 2 requests", got, err, len(sent))
	}
	bad := []Call{{Raw: "ab", Finish: "stop", ReplyTok: 7}}
	if got, err := CountAll(srv.URL+"/nope", uniqueModel(), bad); err == nil || got[0].Counted || got[0].ReplyTok != 7 {
		t.Errorf("failed count = %+v, %v; want uncounted, ReplyTok untouched, an error", got, err)
	}
}

func TestParseThink(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []llm.Level
		bad  bool
	}{
		{"high, none,low", []llm.Level{llm.High, llm.None, llm.Low}, false},
		{"", []llm.Level{llm.None}, false},
		{"low,max", nil, true},
		{"low,low", nil, true},
	} {
		got, err := ParseThink(c.in)
		if (err != nil) != c.bad || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseThink(%q) = %v, %v; want %v, err %v", c.in, got, err, c.want, c.bad)
		}
	}
}

func TestArms(t *testing.T) {
	var got []string
	for _, a := range Arms([]llm.Level{llm.None, llm.Low}, 0) {
		got = append(got, a.Label("qwen3.5:4b"))
	}
	for _, a := range Arms([]llm.Level{llm.None, llm.High}, 200) {
		got = append(got, a.Label("gemma4:e4b-it-qat"))
	}
	if want := []string{"none", "low~", "none", "high~ budget=200"}; !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %q; want %q", got, want)
	}
	a := Arms([]llm.Level{llm.Low}, 200)[0]
	if th := a.Thinking("qwen3.5:4b", llm.PathListen); th.Effort != "low" || th.Budget != 200 {
		t.Errorf("Thinking = %+v; want effort low, budget 200", th)
	}
	if th := Arms([]llm.Level{llm.None}, 200)[0].Thinking("qwen3.5:4b", llm.PathListen); th.Budget != 0 {
		t.Errorf("none Thinking budget = %d; want 0", th.Budget)
	}
}

func TestOrder(t *testing.T) {
	var got [][]int
	for i := 0; i < 4; i++ {
		got = append(got, Order(3, i))
	}
	if want := [][]int{{0, 1, 2}, {1, 2, 0}, {2, 0, 1}, {0, 1, 2}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Order(3, 0..3) = %v; want %v", got, want)
	}
}

func TestChat(t *testing.T) {
	var sent []openai.ChatCompletionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var q openai.ChatCompletionRequest
		_ = json.NewDecoder(r.Body).Decode(&q)
		sent = append(sent, q)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  SKIP \n","reasoning":"they are chatting"},"finish_reason":"length"}],"usage":{"prompt_tokens":321,"completion_tokens":42}}`))
	}))
	defer srv.Close()
	c, err := Chat(srv.URL+"/v1", openai.ChatCompletionRequest{Model: "qwen3.5:4b", ReasoningEffort: "low", MaxTokens: 280})
	if err != nil {
		t.Fatal(err)
	}
	if c.Reply != "SKIP" || c.Raw != "  SKIP \n" || c.Reasoning != "they are chatting" || c.Finish != "length" || c.Prompt != 321 ||
		c.Completion != 42 || c.Dur < 20*time.Millisecond || c.Counted || c.ReplyTok != 0 {
		t.Errorf("Call = %+v", c)
	}
	if len(sent) != 1 { // no count requests: CountAll runs after the timed calls
		t.Fatalf("requests = %d; want 1", len(sent))
	}
	if q := sent[0]; q.Model != "qwen3.5:4b" || q.ReasoningEffort != "low" || q.MaxTokens != 280 {
		t.Errorf("sent = %+v", q)
	}
	if _, err := Chat(srv.URL, openai.ChatCompletionRequest{}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("wrong path err = %v; want HTTP 404", err)
	}
}
