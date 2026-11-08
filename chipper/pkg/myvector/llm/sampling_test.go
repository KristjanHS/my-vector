package llm

import (
	"testing"

	"github.com/sashabaranov/go-openai"
)

func TestSamplingFor(t *testing.T) {
	for _, c := range []struct {
		provider, model string
		want            Sampling
	}{
		{Ollama, "qwen3.5:4b", Sampling{"qwen3.5", 0.7, 0.8, 1.5}},
		{Ollama, "hf.co/unsloth/Qwen3.5-4B-GGUF:Q4_K_M", Sampling{"qwen3.5", 0.7, 0.8, 1.5}},
		{Ollama, "gemma4:e4b-it-qat", Sampling{"gemma4", 1, 0.95, 0}},
		{Ollama, "gemma3:4b-it-qat", Sampling{"", 1, 1, 0}},
		{Ollama, "cas/mistral-7b-instruct-v0.3:latest", Sampling{"", 1, 1, 0}},
		{Claude, "haiku-latest", Sampling{"claude", 1, 0, 0}},
		{"", "claude-haiku-5-5", Sampling{"claude", 1, 0, 0}},
		{"", "anthropic/claude-haiku-5-5", Sampling{"claude", 1, 0, 0}},
	} {
		if got := SamplingFor(c.provider, c.model); got != c.want {
			t.Errorf("SamplingFor(%q, %q) = %+v, want %+v", c.provider, c.model, got, c.want)
		}
	}
}

func TestSamplingApply(t *testing.T) {
	r := openai.ChatCompletionRequest{Temperature: 1, TopP: 1, PresencePenalty: 0.3}
	SamplingFor(Ollama, "qwen3.5:4b").Apply(&r)
	if r.Temperature != 0.7 || r.TopP != 0.8 || r.PresencePenalty != 1.5 {
		t.Errorf("applied %v %v %v", r.Temperature, r.TopP, r.PresencePenalty)
	}
}

func TestProbeSampling(t *testing.T) {
	defer func() { ProbeSampling = nil }()
	arm := Sampling{Family: "probe", Temperature: 0.4, TopP: 0.9}
	ProbeSampling = func(_, _ string) (Sampling, bool) { return arm, true }
	if got := SamplingFor(Ollama, "qwen3.5:4b"); got != arm {
		t.Errorf("Ollama under ProbeSampling = %+v, want %+v", got, arm)
	}
	if got := SamplingFor(Claude, "haiku-latest"); got.Family != "claude" {
		t.Errorf("Claude under ProbeSampling = %+v, want the claude row", got)
	}
}
