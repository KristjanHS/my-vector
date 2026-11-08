package main

import (
	"reflect"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	openai "github.com/sashabaranov/go-openai"
)

// TestRequestNone pins the -think none request to the pre-Stage-1.3 ask() request.
func TestRequestNone(t *testing.T) {
	none := probekit.Arms([]llm.Level{llm.None}, 200)[0]
	for _, m := range []string{"qwen3.5:4b", "gemma4:e4b-it-qat"} {
		want := openai.ChatCompletionRequest{Model: m, ReasoningEffort: "none", MaxTokens: 80, Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: listen.Prompt}, {Role: openai.ChatMessageRoleUser, Content: "hi"}}}
		llm.SamplingFor(llm.Ollama, m).Apply(&want)
		if got := request(m, listen.Prompt, "hi", none.Thinking(m, llm.PathListen)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: request = %+v; want %+v", m, got, want)
		}
	}
}
