package vars

import (
	"path/filepath"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// The saved prompt templates live in myvectorPrompt.json beside apiConfig.json; the page warns when openai_prompt is set.
func init() {
	prompt.Path = func() string { return filepath.Join(filepath.Dir(ApiConfigPath), "myvectorPrompt.json") }
	prompt.UpstreamPrompt = func() string { return APIConfig.Knowledge.OpenAIPrompt }
}
