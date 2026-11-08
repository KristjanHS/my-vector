package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

// configBytes is myvectorConfig.json (lang.Path(), read on every call); a var so tests fake it.
var configBytes = func() ([]byte, error) { return os.ReadFile(lang.Path()) }

// Model is the saved "memory_model"; missing, empty or a bad file gives "ollama:" + llm.DefaultETModel.
func Model() string {
	var f struct {
		Model string `json:"memory_model"`
	}
	if b, err := configBytes(); err == nil {
		json.Unmarshal(b, &f)
	}
	if m := strings.TrimSpace(f.Model); m != "" {
		return m
	}
	return "ollama:" + llm.DefaultETModel
}

// Off stops all capture and every pass (M5).
func Off() bool { return Model() == "off" }

// Target is Model's request target (M1, M2); false = off, an unknown value, or claude: with no key (logged).
func Target(c llm.Config, l string, kg llm.KG) (llm.Target, bool) {
	m := Model()
	p, model, _ := strings.Cut(m, ":")
	switch {
	case m == "off":
		return llm.Target{}, false
	case p == llm.Ollama && model == "":
		return llm.ResolveOllama(c, l, kg, llm.DefaultETModel)
	case p == llm.Ollama:
		return llm.ResolveOllama(c, l, kg, model)
	case p == llm.Claude && model != "" && c.ClaudeKey == "":
		logf("myvector: memory model " + m + " has no Claude key — pass skipped")
		return llm.Target{}, false
	case p == llm.Claude && model != "":
		return llm.Target{Provider: llm.Claude, BaseURL: llm.AnthropicBase + "/v1", Key: c.ClaudeKey, Model: model}, true
	}
	logf("myvector: memory model " + strconv.Quote(m) + " unknown — pass skipped")
	return llm.Target{}, false
}

// Ask is one thinking-off, temperature-0 call to t only — an error never retries on the other provider (M2).
func Ask(ctx context.Context, t llm.Target, system, user string, replyCap int) (string, error) {
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: system}, {Role: openai.ChatMessageRoleUser, Content: user}}}
	llm.ThinkingFor(t.Provider, t.Model, llm.PathChat, llm.None).Apply(&r, replyCap)
	r.MaxTokens = replyCap
	r.Temperature = math.SmallestNonzeroFloat32 // go-openai drops a literal 0 (omitempty)
	res, err := llm.NewClient(t).CreateChatCompletion(ctx, r)
	if err != nil || len(res.Choices) == 0 {
		return "", fmt.Errorf("memory model %s: no answer: %v", t.Model, err)
	}
	return strings.TrimSpace(res.Choices[0].Message.Content), nil
}
