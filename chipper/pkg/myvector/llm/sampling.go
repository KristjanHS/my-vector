package llm

import (
	"strings"

	"github.com/sashabaranov/go-openai"
)

// Sampling is one model family's sampling with thinking off. A zero field is not sent (go-openai omitempty),
// so the model's Ollama Modelfile value applies. The prompt text stays the same for every model:
// Ollama's renderer writes the turn tokens and end token, so only these request fields differ per model.
type Sampling struct {
	Family                             string
	Temperature, TopP, PresencePenalty float32
}

// families: model-card values for chat with thinking off (docs/model-prompt-formats.md); apertus is
// chainprobe arm S (follow-up-listening plan P8). top_k can't go over /v1 (Modelfile: qwen3.5 20, gemma4 64).
var families = []Sampling{
	{"qwen3.5", 0.7, 0.8, 1.5},
	{"gemma4", 1, 0.95, 0},
	{"apertus", 0.4, 0.9, 0},
}

// ProbeSampling is nil in production; a probe sets it so its arm's sampling replaces a non-Claude model's.
var ProbeSampling func(provider, model string) (Sampling, bool)

// SamplingFor is model's family Sampling, or ProbeSampling's for a non-Claude model while a probe sets it.
func SamplingFor(provider, model string) Sampling {
	s := familySampling(provider, model)
	if ProbeSampling != nil && s.Family != "claude" {
		if p, ok := ProbeSampling(provider, model); ok {
			return p
		}
	}
	return s
}

// familySampling is model's Sampling, matched on the name after any "namespace/" prefix.
// A Claude provider or claude* model gets temperature 1 and no top_p (Anthropic rejects top_p);
// any other model keeps upstream wire-pod's temperature 1, top_p 1.
func familySampling(provider, model string) Sampling {
	m := strings.ToLower(model)
	m = m[strings.LastIndex(m, "/")+1:]
	if provider == Claude || strings.HasPrefix(m, "claude") {
		return Sampling{Family: "claude", Temperature: 1}
	}
	for _, f := range families {
		if strings.HasPrefix(m, f.Family) {
			return f
		}
	}
	return Sampling{Temperature: 1, TopP: 1}
}

// Apply sets r's temperature, top_p and presence_penalty to s.
func (s Sampling) Apply(r *openai.ChatCompletionRequest) {
	r.Temperature, r.TopP, r.PresencePenalty = s.Temperature, s.TopP, s.PresencePenalty
}
