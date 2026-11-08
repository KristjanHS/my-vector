package probekit

import (
	"encoding/json"
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
		calls = append(calls, Call{Reply: "ok", Finish: "stop", Prompt: 100, Completion: 1, Dur: ms(i * 100)})
	}
	calls[0] = Call{Reply: "abcdefghi", Reasoning: "hmm", Finish: "stop", Prompt: 300, Completion: 50, Dur: ms(100)} // 9 runes → 3 tokens
	calls[1].Finish = "length"
	calls[2].Reply = ""
	calls[3].Reasoning, calls[3].Completion = "x", 21 // 21 − ceil(2/4) = 20
	s := Summarize(calls, 4)
	want := Summary{N: 10, P50: ms(500), P90: ms(900), CutPct: 20, ThinkPct: 20, T25: 0, T50: 0, T90: 20, MaxCtx: 350}
	if s != want {
		t.Errorf("Summarize = %+v; want %+v", s, want)
	}
	if got := Summarize(calls[:1], 4); got.T50 != 47 || got.P50 != ms(100) {
		t.Errorf("one call: T50 %d P50 %v; want 47, 100ms", got.T50, got.P50)
	}
	if got := Summarize([]Call{{Reply: "abcdefgh", Completion: 1}}, 4); got.T50 != 0 { // 1 − 2 floors at 0
		t.Errorf("floor: T50 %d; want 0", got.T50)
	}
	if got := Summarize(nil, 4); got != (Summary{}) {
		t.Errorf("no calls = %+v; want zero", got)
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

func TestChat(t *testing.T) {
	var sent openai.ChatCompletionRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  SKIP \n","reasoning":"they are chatting"},"finish_reason":"length"}],"usage":{"prompt_tokens":321,"completion_tokens":42}}`))
	}))
	defer srv.Close()
	c, err := Chat(srv.URL+"/v1", openai.ChatCompletionRequest{Model: "qwen3.5:4b", ReasoningEffort: "low", MaxTokens: 280})
	if err != nil {
		t.Fatal(err)
	}
	if c.Reply != "SKIP" || c.Reasoning != "they are chatting" || c.Finish != "length" || c.Prompt != 321 || c.Completion != 42 || c.Dur < 20*time.Millisecond {
		t.Errorf("Call = %+v", c)
	}
	if sent.Model != "qwen3.5:4b" || sent.ReasoningEffort != "low" || sent.MaxTokens != 280 {
		t.Errorf("sent = %+v", sent)
	}
	if _, err := Chat(srv.URL, openai.ChatCompletionRequest{}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("wrong path err = %v; want HTTP 404", err)
	}
}
