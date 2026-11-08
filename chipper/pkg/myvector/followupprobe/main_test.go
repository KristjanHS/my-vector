package main

import (
	"reflect"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	openai "github.com/sashabaranov/go-openai"
)

// TestRequestNone pins the -think none request to the pre-Stage-1.3 ask() request.
func TestRequestNone(t *testing.T) {
	none := probekit.Arms([]llm.Level{llm.None}, 100)[0]
	for _, m := range []string{"qwen3.5:4b", "gemma4:e4b-it-qat"} {
		tg := llm.Target{Provider: llm.Ollama, BaseURL: "http://localhost:11434/v1", Key: "ollama", Model: m}
		want := openai.ChatCompletionRequest{Model: m, ReasoningEffort: "none", MaxTokens: 8, Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompts[1]}, {Role: openai.ChatMessageRoleUser, Content: "hi"}}}
		llm.SamplingFor(llm.Ollama, m).Apply(&want)
		if got := request(tg, prompts[1], "hi", none.Thinking(m, llm.PathFollowUp)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: request = %+v; want %+v", m, got, want)
		}
	}
}
