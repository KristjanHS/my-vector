package vars

import (
	"path/filepath"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// The saved prompt templates live in myvectorPrompt.json beside apiConfig.json.
func init() {
	prompt.Path = func() string { return filepath.Join(filepath.Dir(ApiConfigPath), "myvectorPrompt.json") }
}
