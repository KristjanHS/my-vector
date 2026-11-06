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
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
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

// DefaultETVoice is the et-EE T1 and T2 default (Estonian plan E30).
const DefaultETVoice = "mari"

// voices are each language's T1 and T2 choices (plan R8, E30); every name but Stock must be a tts/server.py VOICES key.
var voices = map[string][]string{
	lang.English:  {DefaultVoice, "cori", Stock},
	lang.Estonian: {DefaultETVoice},
}

// VoicesFor is language l's T1 and T2 choices; nil for an unsupported l.
func VoicesFor(l string) []string { return voices[l] }

// Settings is each language's T1 (LLM-answer) and T2 (robot's built-in lines) voice and their one slow factor (R16, E30; 1 = normal speed).
type Settings struct {
	T1   string  `json:"t1"`
	T2   string  `json:"t2"`
	T1ET string  `json:"t1_et"`
	T2ET string  `json:"t2_et"`
	Slow float64 `json:"slow"`
}

func Defaults() Settings {
	return Settings{T1: DefaultVoice, T2: DefaultVoice, T1ET: DefaultETVoice, T2ET: DefaultETVoice, Slow: DefaultSlow}
}

// For is s with language l's pair as T1 and T2, the pair Speak and PushT2 read.
func (s Settings) For(l string) Settings {
	if l == lang.Estonian {
		s.T1, s.T2 = s.T1ET, s.T2ET
	}
	return s
}

func (s Settings) Validate() error {
	for _, f := range []struct{ name, v, lang string }{
		{"t1", s.T1, lang.English}, {"t2", s.T2, lang.English}, {"t1_et", s.T1ET, lang.Estonian}, {"t2_et", s.T2ET, lang.Estonian},
	} {
		ok := false
		for _, v := range voices[f.lang] {
			ok = ok || f.v == v
		}
		if !ok {
			return fmt.Errorf("%s: one of %v", f.name, voices[f.lang])
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

// Save validates s and writes its keys to the settings file atomically, keeping the others ("lang", "et_model").
func Save(s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return conf.Update(SettingsPath(), map[string]any{"t1": s.T1, "t2": s.T2, "t1_et": s.T1ET, "t2_et": s.T2ET, "slow": s.Slow})
}

// PushTimeout bounds one voice-push.sh run (its SSH ConnectTimeout is 8 s).
var PushTimeout = 15 * time.Second

var (
	// PushT2 writes s's T2 + slow to the robot (plan R15); tests replace it so none runs SSH.
	PushT2 = pushT2
	// ReloadIntents reloads wire-pod's intents after a language switch; tests replace it.
	ReloadIntents = reloadIntents
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

// reloadIntents loads ./intent-data/<vars.ForkLanguage()>.json; on an error the old intents stay.
func reloadIntents() error {
	l, err := vars.LoadIntents()
	if err == nil {
		vars.IntentList = l
	}
	return err
}

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
	Lang    string              `json:"lang"`
	Langs   []string            `json:"langs"`
	Voices  map[string][]string `json:"voices"`
	SlowMin float64             `json:"slowMin"`
	SlowMax float64             `json:"slowMax"`
	// PushError is why a saved T2 did not reach the robot; the save itself stands.
	PushError string `json:"pushError,omitempty"`
}

type settingsRequest struct {
	Settings
	Lang string `json:"lang"`
}

func (q *settingsRequest) valid() error {
	if !lang.Valid(q.Lang) {
		return fmt.Errorf("lang: one of %v", lang.Supported)
	}
	return q.Validate()
}

// SettingsHandler serves SettingsAPI: GET reads, POST (application/json Settings + lang) validates, saves,
// switches the language (reloading intents) and pushes that language's T2.
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	pushErr := ""
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var q settingsRequest
		if !decode(w, r, &q, q.valid) {
			return
		}
		if err := Save(q.Settings); err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if q.Lang != lang.Current() {
			if err := lang.Save(q.Lang); err != nil {
				http.Error(w, "save lang: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := ReloadIntents(); err != nil {
				logger.Println("myvector voice settings: " + q.Lang + " intents not loaded: " + err.Error())
			}
		}
		if err := PushT2(r.Context(), q.Settings.For(q.Lang)); err != nil {
			pushErr = err.Error()
			logger.Println("myvector voice settings: saved; robot not updated: " + pushErr)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsReply{Current(), lang.Current(), lang.Supported, voices, SlowMin, SlowMax, pushErr})
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
	Lang string `json:"lang"`
}

func (d *demoRequest) valid() error {
	if n := len([]rune(d.Text)); n == 0 || n > DemoMaxText {
		return fmt.Errorf("text: 1 to %d characters", DemoMaxText)
	}
	if d.Lang == "" {
		d.Lang = lang.Current()
	} else if !lang.Valid(d.Lang) {
		return fmt.Errorf("lang: one of %v", lang.Supported)
	}
	return d.Validate()
}

// DemoHandler serves DemoAPI (plan R14): POST application/json {t1, t1_et, slow, text, lang} speaks text on robotFor's
// robot in lang's (default the current one) unsaved T1 and replies {"voice": <the voice that spoke>}; a robot or speech error is a 502.
func DemoHandler(robotFor func(*http.Request) (context.Context, *vector.Vector, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		d := demoRequest{Settings: Settings{T2: DefaultVoice, T1ET: DefaultETVoice, T2ET: DefaultETVoice}} // the demo speaks T1; t2, t1_et, t2_et may be omitted
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
		used, err := Speak(ctx, v, d.Text, d.Settings.For(d.Lang))
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
