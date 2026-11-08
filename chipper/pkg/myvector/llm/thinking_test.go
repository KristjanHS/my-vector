package llm

import (
	"os"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

func TestThinkingFor(t *testing.T) {
	qwenOff, gemma := Sampling{"qwen3.5", 0.7, 0.8, 1.5}, Sampling{"gemma4", 1, 0.95, 0}
	for _, c := range []struct {
		name, provider, model string
		level                 Level
		want                  Thinking
	}{
		{"qwen none", Ollama, "qwen3.5:4b", None, Thinking{Effort: "none", Sampling: qwenOff}},
		{"qwen medium", Ollama, "qwen3.5:4b", Medium, Thinking{Effort: "medium", Budget: 0, Sampling: Sampling{"qwen3.5", 1, 0.95, 1.5}}},
		{"gemma4 high", Ollama, "gemma4:e4b-it-qat", High, Thinking{Effort: "high", Sampling: gemma}},
		{"claude provider", Claude, "haiku-latest", High, Thinking{}},
		{"claude model", "", "anthropic/claude-haiku-5-5", Low, Thinking{}},
		{"unknown model", Ollama, "cas/mistral-7b-instruct-v0.3:latest", High, Thinking{Effort: "none", Sampling: Sampling{"", 1, 1, 0}}},
		{"unknown level", Ollama, "qwen3.5:4b", Level("max"), Thinking{Effort: "none", Sampling: qwenOff}},
	} {
		if got := ThinkingFor(c.provider, c.model, PathChat, c.level); got != c.want {
			t.Errorf("%s: ThinkingFor = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestThinkingCeiling(t *testing.T) {
	c := Config{Think: map[Path]Level{PathChat: Medium}}
	if got := c.Thinking(Ollama, "qwen3.5:4b", PathChat); got.Effort != "none" {
		t.Errorf("saved medium over ceiling none: Effort %q, want none", got.Effort)
	}
	if got := (Config{}).Thinking(Ollama, "gemma4:e4b-it-qat", PathListen); got.Effort != string(DefaultThink[PathListen]) {
		t.Errorf("no saved level: Effort %q, want DefaultThink %q", got.Effort, DefaultThink[PathListen])
	}
}

func TestProbeThinking(t *testing.T) {
	c := Config{Think: map[Path]Level{PathChat: High}}
	if got := c.Thinking(Ollama, "qwen3.5:4b", PathChat); got.Effort != "none" {
		t.Errorf("nil hook: Effort %q, want the ceiling's none", got.Effort)
	}
	want := Thinking{Effort: "high", Budget: 200}
	ProbeThinking = func(_, _ string, p Path) (Thinking, bool) { return want, p == PathChat }
	defer func() { ProbeThinking = nil }()
	if got := c.Thinking(Ollama, "qwen3.5:4b", PathChat); got != want {
		t.Errorf("hook set: %+v, want %+v", got, want)
	}
	if got := c.Thinking(Ollama, "qwen3.5:4b", PathListen); got.Effort != "none" {
		t.Errorf("hook not ok: Effort %q, want none", got.Effort)
	}
}

func TestThinkingApply(t *testing.T) {
	r := openai.ChatCompletionRequest{MaxCompletionTokens: 9}
	Thinking{Effort: "high", Budget: 400, Sampling: Sampling{"qwen3.5", 1, 0.95, 1.5}}.Apply(&r, 80)
	if r.ReasoningEffort != "high" || r.MaxTokens != 480 || r.MaxCompletionTokens != 0 || r.Temperature != 1 || r.TopP != 0.95 {
		t.Errorf("applied %+v", r)
	}
	r = openai.ChatCompletionRequest{MaxCompletionTokens: 300}
	Thinking{}.Apply(&r, 80)
	if r.ReasoningEffort != "" || r.MaxTokens != 0 || r.MaxCompletionTokens != 300 {
		t.Errorf("zero Thinking changed the request: %+v", r)
	}
}

func TestLoadThink(t *testing.T) {
	if err := os.WriteFile(tempPath(t), []byte(`{"think":{"chat":"high","listen":"bogus"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load()
	if c.Think[PathChat] != High {
		t.Errorf("chat = %q, want high", c.Think[PathChat])
	}
	if _, ok := c.Think[PathListen]; ok {
		t.Errorf("bad level kept: %q", c.Think[PathListen])
	}
	key := strings.Repeat("k", 24)
	for _, think := range []string{`{"chat":true}`, `"low"`} {
		file := `{"llm":{"en-US":{"provider":"ollama","model":"qwen3.5:4b"}},"claude_key":"` + key + `","think":` + think + `}`
		if err := os.WriteFile(tempPath(t), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		c := Load()
		if c.Profiles["en-US"].Model != "qwen3.5:4b" || c.ClaudeKey != key || len(c.Think) != 0 {
			t.Errorf("think %s: Load = %+v; want the profile and key kept, no levels", think, c)
		}
	}
}

// TestThinkingBudgetCeiling swaps in a row with distinct budgets and a medium ceiling (the shipped rows are all 0 / none).
func TestThinkingBudgetCeiling(t *testing.T) {
	saved := thinkRows
	t.Cleanup(func() { thinkRows = saved })
	thinkRows = []thinkRow{{family: "qwen3.5", on: Sampling{"qwen3.5", 1, 0.95, 1.5},
		budget:  map[Path][3]int{PathListen: {100, 200, 300}},
		ceiling: map[Path]Level{PathListen: Medium}}}
	for _, c := range []struct {
		level  Level
		budget int
	}{{Low, 100}, {High, 300}} {
		if got := ThinkingFor(Ollama, "qwen3.5:4b", PathListen, c.level); got.Budget != c.budget {
			t.Errorf("ThinkingFor %s: Budget %d, want %d", c.level, got.Budget, c.budget)
		}
	}
	for _, c := range []struct {
		saved, want Level
		budget      int
	}{{Low, Low, 100}, {Medium, Medium, 200}, {High, Medium, 200}} {
		got := Config{Think: map[Path]Level{PathListen: c.saved}}.Thinking(Ollama, "qwen3.5:4b", PathListen)
		if got.Effort != string(c.want) || got.Budget != c.budget {
			t.Errorf("saved %s: %+v, want effort %s budget %d", c.saved, got, c.want, c.budget)
		}
	}
}
