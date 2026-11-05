package wirepod_ttr

import "strings"

// forkTopP is the top_p wire-pod sends with a knowledge-graph request.
// Claude models reject any top_p over Anthropic's OpenAI-compatible endpoint
// (400 "`top_p` is deprecated for this model"), so for them it returns 0,
// which go-openai drops from the JSON (omitempty). Every other model keeps
// upstream's 1.
func forkTopP(model string) float32 {
	if strings.HasPrefix(strings.ToLower(model), "claude") {
		return 0
	}
	return 1
}

// forkFlushUnpunctuated: a stream that ends before any . ? ! (e.g. a bare
// "{{getImage||front}}") becomes one chunk instead of "llm returned no response".
func forkFlushUnpunctuated(chunks []string, tail string) ([]string, bool) {
	if len(chunks) > 0 || strings.TrimSpace(tail) == "" {
		return chunks, false
	}
	return []string{strings.TrimSpace(tail)}, true
}
