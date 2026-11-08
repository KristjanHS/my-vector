package probekit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

// ParseThink is a -think list of none|low|medium|high in order; "" is [none].
func ParseThink(s string) ([]llm.Level, error) {
	if strings.TrimSpace(s) == "" {
		return []llm.Level{llm.None}, nil
	}
	var out []llm.Level
	for _, x := range strings.Split(s, ",") {
		l, ok := llm.ParseLevel(strings.TrimSpace(x))
		if !ok {
			return nil, fmt.Errorf("-think: %q is not none, low, medium or high", x)
		}
		for _, y := range out {
			if y == l {
				return nil, fmt.Errorf("-think: %q repeated", x)
			}
		}
		out = append(out, l)
	}
	return out, nil
}

// Arm is one thinking level; Budget > 0 overrides the table budget (-think-budget).
type Arm struct {
	Level  llm.Level
	Budget int
}

// Arms is one Arm per level; budget applies to every level but none.
func Arms(levels []llm.Level, budget int) []Arm {
	out := make([]Arm, len(levels))
	for i, l := range levels {
		out[i] = Arm{Level: l}
		if l != llm.None && budget > 0 {
			out[i].Budget = budget
		}
	}
	return out
}

// Thinking is model's ThinkingFor at a's level on path, Budget overridden when set.
func (a Arm) Thinking(model string, path llm.Path) llm.Thinking {
	t := llm.ThinkingFor(llm.Ollama, model, path, a.Level)
	if a.Budget > 0 {
		t.Budget = a.Budget
	}
	return t
}

// Label is the level, "~" when model's row is not Native (emulated), then " budget=N" when overridden.
func (a Arm) Label(model string) string {
	s := string(a.Level)
	if a.Level != llm.None && !a.Thinking(model, llm.PathChat).Native {
		s += "~"
	}
	if a.Budget > 0 {
		s += fmt.Sprintf(" budget=%d", a.Budget)
	}
	return s
}

// Call is one /v1/chat/completions result, reasoning included (go-openai drops it); ReplyTok is CountReply's.
type Call struct {
	Reply, Reasoning, Finish     string
	Prompt, Completion, ReplyTok int
	Dur                          time.Duration
}

var chatClient = &http.Client{Timeout: 120 * time.Second}

// Chat posts r to baseURL+"/chat/completions" (Ollama /v1), parses the raw body and counts the reply (CountReply).
func Chat(baseURL string, r openai.ChatCompletionRequest) (Call, error) {
	c, content, err := post(baseURL, r)
	if err != nil {
		return c, err
	}
	c.ReplyTok, err = CountReply(baseURL, r.Model, content, c.Finish)
	return c, err
}

var (
	emptyMu sync.Mutex
	empty   = map[string]int{} // baseURL + model → prompt_tokens of {user: ""}
)

// CountReply is content's exact token count on model (T13): prompt_tokens({user: content}) − prompt_tokens({user: ""}),
// + 1 for the end-of-turn token on finish "stop".
func CountReply(baseURL, model, content, finish string) (int, error) {
	n := 0
	if content != "" {
		emptyMu.Lock()
		e, ok := empty[baseURL+" "+model]
		emptyMu.Unlock()
		if !ok {
			var err error
			if e, err = promptTokens(baseURL, model, ""); err != nil {
				return 0, err
			}
			emptyMu.Lock()
			empty[baseURL+" "+model] = e
			emptyMu.Unlock()
		}
		p, err := promptTokens(baseURL, model, content)
		if err != nil {
			return 0, err
		}
		n = p - e
	}
	if finish == "stop" {
		n++
	}
	return n, nil
}

func promptTokens(baseURL, model, content string) (int, error) {
	c, _, err := post(baseURL, map[string]any{"model": model, "reasoning_effort": "none", "max_tokens": 1, // a map: go-openai omits "" content, Ollama 400s
		"messages": []map[string]string{{"role": openai.ChatMessageRoleUser, "content": content}}})
	if err != nil {
		return 0, fmt.Errorf("count: %w", err)
	}
	return c.Prompt, nil
}

// post sends r (a request body) and returns the Call (Reply trimmed) and the untrimmed content.
func post(baseURL string, r any) (Call, string, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return Call{}, "", err
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("content-type", "application/json")
	start := time.Now()
	res, err := chatClient.Do(req)
	if err != nil {
		return Call{}, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	dur := time.Since(start)
	if res.StatusCode != http.StatusOK {
		return Call{}, "", fmt.Errorf("chat HTTP %d: %.200s", res.StatusCode, raw)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Call{}, "", fmt.Errorf("chat reply: %v", err)
	}
	if len(out.Choices) == 0 {
		return Call{}, "", errors.New("chat reply has no choices")
	}
	ch := out.Choices[0]
	return Call{Reply: strings.TrimSpace(ch.Message.Content), Reasoning: ch.Message.Reasoning, Finish: ch.FinishReason,
		Prompt: out.Usage.PromptTokens, Completion: out.Usage.CompletionTokens, Dur: dur}, ch.Message.Content, nil
}

// Order is arm indices 0..n-1 starting at item mod n: item i runs arm i mod n first (no fixed arm order per prompt).
func Order(n, item int) []int {
	out := make([]int, n)
	for k := range out {
		out[k] = (item + k) % n
	}
	return out
}

// Summary is one arm's calls: latency, cut-off (T6), thinking share and tokens (T13), count exactness (2.1), peak sizes.
type Summary struct {
	N                          int
	P50, P90                   time.Duration
	CutPct, ThinkPct, ExactPct float64
	T25, T50, T90              int
	MaxPrompt, MaxCtx          int
}

// Summarize is calls' Summary; thinking tokens = Completion − ReplyTok, floored at 0; exact = Completion == ReplyTok.
func Summarize(calls []Call) Summary {
	s := Summary{N: len(calls)}
	if s.N == 0 {
		return s
	}
	ds := make([]time.Duration, s.N)
	ts := make([]int, s.N)
	cut, think, exact := 0, 0, 0
	for i, c := range calls {
		ds[i] = c.Dur
		if c.Finish == "length" || c.Reply == "" {
			cut++
		}
		if c.Reasoning != "" {
			think++
		}
		if c.Completion == c.ReplyTok {
			exact++
		}
		if ts[i] = c.Completion - c.ReplyTok; ts[i] < 0 {
			ts[i] = 0
		}
		if c.Prompt > s.MaxPrompt {
			s.MaxPrompt = c.Prompt
		}
		if ctx := c.Prompt + c.Completion; ctx > s.MaxCtx {
			s.MaxCtx = ctx
		}
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	sort.Ints(ts)
	s.P50, s.P90 = ds[rank(50, s.N)], ds[rank(90, s.N)]
	s.T25, s.T50, s.T90 = ts[rank(25, s.N)], ts[rank(50, s.N)], ts[rank(90, s.N)]
	pct := func(k int) float64 { return 100 * float64(k) / float64(s.N) }
	s.CutPct, s.ThinkPct, s.ExactPct = pct(cut), pct(think), pct(exact)
	return s
}

// rank is the nearest-rank index of percentile p in n sorted values: ceil(p/100·n) − 1.
func rank(p, n int) int {
	if i := (p*n+99)/100 - 1; i > 0 {
		return i
	}
	return 0
}

// Line is s as one per-arm line.
func (s Summary) Line(label string) string {
	return fmt.Sprintf("%-18s n %d  p50/p90 %d/%d ms  cut %.0f%%  think %.0f%%  T %d/%d/%d  exact %.0f%%  max prompt %d  max ctx %d",
		label, s.N, s.P50.Milliseconds(), s.P90.Milliseconds(), s.CutPct, s.ThinkPct, s.T25, s.T50, s.T90, s.ExactPct,
		s.MaxPrompt, s.MaxCtx)
}
