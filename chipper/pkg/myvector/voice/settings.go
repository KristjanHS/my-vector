package voice

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

const (
	Stock            = "stock" // the robot's own voice; slow sets its DurationScalar (plan R12)
	DefaultSlow      = 1.2     // plan R11
	SlowMin, SlowMax = 0.5, 2.0 // tts/server.py SLOW_MIN, SLOW_MAX
	SettingsAPI      = "/myvector/api/voice"
	SettingsPage     = "/myvector/voice"
	DemoAPI          = "/myvector/api/voice/test"
	DemoMaxText      = 599              // as fork_say
	DemoTimeout      = 90 * time.Second // DemoMaxText at SlowMax
)

// Voices are the T1 choices (plan R8); every name but Stock must be a tts/server.py VOICES key.
var Voices = []string{DefaultVoice, "af_bella", Stock}

// Settings is the T1 (LLM-answer) voice and its slow factor (1 = normal speed).
type Settings struct {
	T1   string  `json:"t1"`
	Slow float64 `json:"slow"`
}

func Defaults() Settings { return Settings{T1: DefaultVoice, Slow: DefaultSlow} }

func (s Settings) Validate() error {
	ok := false
	for _, v := range Voices {
		ok = ok || s.T1 == v
	}
	if !ok {
		return fmt.Errorf("t1: one of %v", Voices)
	}
	if !(s.Slow >= SlowMin && s.Slow <= SlowMax) { // NaN fails too
		return fmt.Errorf("slow: %g to %g", SlowMin, SlowMax)
	}
	return nil
}

var (
	// SettingsPath is the settings file, beside wire-pod's apiConfig.json (gitignored).
	SettingsPath = func() string { return filepath.Join(filepath.Dir(vars.ApiConfigPath), "myvectorConfig.json") }
	saveMu       sync.Mutex
	//go:embed settings.html
	page []byte
)

// Current reads the saved settings; a missing file gives Defaults, an unreadable or invalid one is logged and gives Defaults.
func Current() Settings {
	b, err := os.ReadFile(SettingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults()
	}
	s := Defaults()
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err == nil {
		err = s.Validate()
	}
	if err != nil {
		logger.Println("myvector voice settings: using defaults: " + err.Error())
		return Defaults()
	}
	return s
}

// Save validates s and replaces the settings file atomically.
func Save(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	saveMu.Lock()
	defer saveMu.Unlock()
	path := SettingsPath()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".myvectorConfig-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Chmod(0644)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	return err
}

type settingsReply struct {
	Settings
	Voices  []string `json:"voices"`
	SlowMin float64  `json:"slowMin"`
	SlowMax float64  `json:"slowMax"`
}

// SettingsHandler serves SettingsAPI: GET reads, POST (application/json Settings) validates and saves.
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var s Settings
		if !decode(w, r, &s, func() error { return s.Validate() }) { // not s.Validate: that binds the empty s
			return
		}
		if err := Save(s); err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsReply{Current(), Voices, SlowMin, SlowMax})
}

// decode reads an application/json POST body into v and runs valid on it; on failure it has written the error.
// The JSON-only body makes a cross-site form post fail its CORS preflight.
func decode(w http.ResponseWriter, r *http.Request, v any, valid func() error) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		err = valid()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

type demoRequest struct {
	Settings
	Text string `json:"text"`
}

func (d *demoRequest) valid() error {
	if n := len([]rune(d.Text)); n == 0 || n > DemoMaxText {
		return fmt.Errorf("text: 1 to %d characters", DemoMaxText)
	}
	return d.Validate()
}

// DemoHandler serves DemoAPI (plan R14): POST application/json {t1, slow, text} speaks text on robotFor's robot
// in that unsaved voice and replies {"voice": <the voice that spoke>}; a robot or speech error is a 502.
func DemoHandler(robotFor func(*http.Request) (context.Context, *vector.Vector, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		var d demoRequest
		if !decode(w, r, &d, d.valid) {
			return
		}
		ctx, v, err := robotFor(r)
		if err != nil {
			http.Error(w, "robot: "+err.Error(), http.StatusBadGateway)
			return
		}
		ctx, cancel := context.WithTimeout(ctx, DemoTimeout)
		defer cancel()
		used, err := Speak(ctx, v, d.Text, d.Settings)
		if err != nil {
			http.Error(w, used+": "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"voice": used})
	}
}

// PageHandler serves the settings page at SettingsPage.
func PageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(page)
}
