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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

const (
	Stock            = "stock"  // the robot's own voice; slow sets its DurationScalar (plan R12)
	DefaultSlow      = 1.2      // plan R11
	SlowMin, SlowMax = 0.5, 2.0 // tts/server.py SLOW_MIN, SLOW_MAX
	SettingsAPI      = "/myvector/api/voice"
	SettingsPage     = "/myvector/voice"
	DemoAPI          = "/myvector/api/voice/test"
	DemoMaxText      = 599              // as fork_say
	DemoTimeout      = 90 * time.Second // DemoMaxText at SlowMax
)

// Voices are the T1 and T2 choices (plan R8); every name but Stock must be a tts/server.py VOICES key.
var Voices = []string{DefaultVoice, "cori", "mari", Stock}

// Settings is the T1 (LLM-answer) and T2 (robot's built-in lines) voice and their one slow factor (R16; 1 = normal speed).
type Settings struct {
	T1   string  `json:"t1"`
	T2   string  `json:"t2"`
	Slow float64 `json:"slow"`
}

func Defaults() Settings { return Settings{T1: DefaultVoice, T2: DefaultVoice, Slow: DefaultSlow} }

func (s Settings) Validate() error {
	for _, f := range []struct{ name, v string }{{"t1", s.T1}, {"t2", s.T2}} {
		ok := false
		for _, v := range Voices {
			ok = ok || f.v == v
		}
		if !ok {
			return fmt.Errorf("%s: one of %v", f.name, Voices)
		}
	}
	if !(s.Slow >= SlowMin && s.Slow <= SlowMax) { // NaN fails too
		return fmt.Errorf("slow: %g to %g", SlowMin, SlowMax)
	}
	return nil
}

var (
	// SettingsPath is the settings file, beside wire-pod's apiConfig.json (gitignored).
	SettingsPath = func() string { return filepath.Join(filepath.Dir(vars.ApiConfigPath), "myvectorConfig.json") }
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

// Save validates s and writes its keys to the settings file atomically, keeping the others ("lang").
func Save(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return conf.Update(SettingsPath(), map[string]any{"t1": s.T1, "t2": s.T2, "slow": s.Slow})
}

// PushTimeout bounds one voice-push.sh run (its SSH ConnectTimeout is 8 s).
var PushTimeout = 15 * time.Second

var (
	// PushT2 writes s's T2 + slow to the robot (plan R15); tests replace it so none runs SSH.
	PushT2 = pushT2
	// pushScript is robot/firmware/voice-push.sh in the nearest ancestor of the cwd (wire-pod's is chipper/).
	pushScript = func() (string, error) { return findUp(filepath.Join("robot", "firmware", "voice-push.sh")) }
)

func findUp(rel string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if p := filepath.Join(dir, rel); fileExists(p) {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s above the working directory", rel)
		}
		dir = parent
	}
}

func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

// pushT2 runs voice-push.sh <t2> <slow>; its error carries the script's last output line.
func pushT2(ctx context.Context, s Settings) error {
	script, err := pushScript()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, PushTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, s.T2, strconv.FormatFloat(s.Slow, 'g', -1, 64))
	cmd.WaitDelay = time.Second // an orphaned ssh holding the output pipe must not outlive the timeout
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("voice-push.sh: no answer in %v", PushTimeout)
	}
	if err != nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("voice-push.sh: %v: %s", err, lines[len(lines)-1])
	}
	return nil
}

type settingsReply struct {
	Settings
	Voices  []string `json:"voices"`
	SlowMin float64  `json:"slowMin"`
	SlowMax float64  `json:"slowMax"`
	// PushError is why a saved T2 did not reach the robot; the save itself stands.
	PushError string `json:"pushError,omitempty"`
}

// SettingsHandler serves SettingsAPI: GET reads, POST (application/json Settings) validates, saves and pushes T2.
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	pushErr := ""
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
		if err := PushT2(r.Context(), s); err != nil {
			pushErr = err.Error()
			logger.Println("myvector voice settings: saved; robot not updated: " + pushErr)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsReply{Current(), Voices, SlowMin, SlowMax, pushErr})
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
		d := demoRequest{Settings: Settings{T2: DefaultVoice}} // the demo speaks T1; t2 may be omitted
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
		release, err := Control(ctx, v)
		if err != nil {
			http.Error(w, "robot control: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer release()
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
