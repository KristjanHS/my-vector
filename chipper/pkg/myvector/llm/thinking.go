package llm

import (
	"strings"

	"github.com/sashabaranov/go-openai"
)

// Level is a thinking level; the name is sent as reasoning_effort (local-thinking-levels.md (docs/plans/) T3).
type Level string

const (
	None   Level = "none"
	Low    Level = "low"
	Medium Level = "medium"
	High   Level = "high"
)

var levels = []Level{None, Low, Medium, High}

// ParseLevel is s as a Level; ok is false for anything else.
func ParseLevel(s string) (Level, bool) {
	for _, l := range levels {
		if string(l) == s {
			return l, true
		}
	}
	return None, false
}

func (l Level) rank() int {
	for i, x := range levels {
		if x == l {
			return i
		}
	}
	return 0
}

// Path is a request path with its own thinking level (T15).
type Path string

const (
	PathChat     Path = "chat"
	PathListen   Path = "listen"
	PathFollowUp Path = "followup"
)

// DefaultThink is each path's level when myvectorConfig.json has no "think" entry.
var DefaultThink = map[Path]Level{PathChat: None, PathListen: None, PathFollowUp: None}

// Thinking is one request's thinking setup; the zero value (Claude) leaves the request alone.
type Thinking struct {
	Effort   string
	Budget   int // tokens added to the reply cap
	Native   bool
	Sampling Sampling
}

type thinkRow struct {
	family  string
	native  bool
	on      Sampling        // sampling with thinking on (T12)
	budget  map[Path][3]int // low, medium, high; 0 = unmeasured (Stage 2.2)
	ceiling map[Path]Level  // highest level that passed Stage 3
}

var thinkRows = []thinkRow{
	{family: "qwen3.5", on: Sampling{"qwen3.5", 1, 0.95, 1.5},
		budget:  map[Path][3]int{},
		ceiling: map[Path]Level{PathChat: None, PathListen: None, PathFollowUp: None}},
	{family: "gemma4", on: Sampling{"gemma4", 1, 0.95, 0},
		budget:  map[Path][3]int{},
		ceiling: map[Path]Level{PathChat: None, PathListen: None, PathFollowUp: None}},
}

func rowFor(model string) (thinkRow, bool) {
	s := SamplingFor(Ollama, model)
	for _, r := range thinkRows {
		if r.family == s.Family {
			return r, true
		}
	}
	return thinkRow{}, false
}

// ThinkingFor is model's setup at level on path, with no ceiling (probes call it directly).
// Claude gets the zero Thinking; an unknown Ollama model or level gets none.
func ThinkingFor(provider, model string, path Path, level Level) Thinking {
	m := strings.ToLower(model)
	if provider == Claude || strings.HasPrefix(m[strings.LastIndex(m, "/")+1:], "claude") {
		return Thinking{}
	}
	off := Thinking{Effort: string(None), Sampling: SamplingFor(Ollama, model)}
	r, ok := rowFor(model)
	if !ok || level.rank() == 0 {
		return off
	}
	return Thinking{Effort: string(level), Budget: r.budget[path][level.rank()-1], Native: r.native, Sampling: r.on}
}

// Thinking is the production setup: the saved "think" level (else DefaultThink), clamped to the model's ceiling (T15).
func (c Config) Thinking(provider, model string, path Path) Thinking {
	level, ok := c.Think[path]
	if !ok {
		level = DefaultThink[path]
	}
	if r, ok := rowFor(model); ok && level.rank() > r.ceiling[path].rank() {
		level = r.ceiling[path]
	}
	return ThinkingFor(provider, model, path, level)
}

// Apply sets r's reasoning_effort, max_tokens = replyCap + Budget, and sampling; the zero Thinking changes nothing.
func (t Thinking) Apply(r *openai.ChatCompletionRequest, replyCap int) {
	if t.Effort == "" {
		return
	}
	r.ReasoningEffort = t.Effort
	r.MaxTokens, r.MaxCompletionTokens = replyCap+t.Budget, 0
	t.Sampling.Apply(r)
}
