package vars

import (
	"path/filepath"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// The language switch lives in myvectorConfig.json beside apiConfig.json (as myvector/voice's settings).
func init() {
	lang.Path = func() string { return filepath.Join(filepath.Dir(ApiConfigPath), "myvectorConfig.json") }
}

// ForkLanguage is the language intents, GetText and custom intents follow: et-EE while the fork switch says so, else wire-pod's own STT.Language.
func ForkLanguage() string {
	if lang.Current() == lang.Estonian {
		return lang.Estonian
	}
	return APIConfig.STT.Language
}
