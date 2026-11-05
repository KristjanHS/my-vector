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
