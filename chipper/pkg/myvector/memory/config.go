package memory

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
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
