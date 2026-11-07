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
	"sync"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/conf"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

const (
	Stock            = "stock"  // the robot's own Acapela voice, without the robot-voice effect since patch 0010; slow sets its DurationScalar (plan R12)
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
	// PushT2 writes language l's T2 + slow + l to the robot (plan R15, E31); tests replace it so none runs SSH.
	PushT2 = pushT2
	// ReloadIntents reloads wire-pod's intents after a language switch; tests replace it.
	ReloadIntents = reloadIntents
	// pushScript is robot/firmware/voice-push.sh in the nearest ancestor of the cwd (wire-pod's is chipper/).
	pushScript = func() (string, error) { return conf.FindUp(filepath.Join("robot", "firmware", "voice-push.sh")) }
)

// reloadIntents loads ./intent-data/<vars.ForkLanguage()>.json; on an error the old intents stay.
func reloadIntents() error {
	l, err := vars.LoadIntents()
	if err == nil {
		vars.IntentList = l
	}
	return err
}

// pushT2 runs voice-push.sh <l's t2> <slow> <l>; its error carries the script's last output line.
func pushT2(ctx context.Context, s Settings, l string) error {
	script, err := pushScript()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, PushTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, s.For(l).T2, strconv.FormatFloat(s.Slow, 'g', -1, 64), l)
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
	// LLM is each language's effective brain (llm.Effective); Defaults is the same with no saved profiles.
	LLM       map[string]llm.Profile `json:"llm"`
	Defaults  map[string]llm.Profile `json:"defaults"`
	OllamaURL string                 `json:"ollamaUrl"`
	ClaudeKey string                 `json:"claudeKey"` // llm.Mask: never the full key
	KGEnabled bool                   `json:"kgEnabled"`
	// LLMSaved is false until the page first saves "llm"; till then Knowledge (provider model) is the brain.
	LLMSaved  bool   `json:"llmSaved"`
	Knowledge string `json:"knowledge"`
	// PushError is why a saved T2 did not reach the robot; the save itself stands.
	PushError string `json:"pushError,omitempty"`
	// IntentError is why the language switch was rolled back (its intents did not load); the other saves stand.
	IntentError string      `json:"intentError,omitempty"`
	Search      searchReply `json:"search"`
}

// searchReply is the W12 search block; keys are llm.Mask'ed.
type searchReply struct {
	Provider     string       `json:"provider"`
	TavilyKey    string       `json:"tavilyKey"`
	OllamaKey    string       `json:"ollamaKey"`
	SearxngURL   string       `json:"searxngUrl"`
	WeatherPlace string       `json:"weatherPlace"`
	Usage        []search.Use `json:"usage"`
}

func currentSearch() searchReply {
	c := search.LoadConfig()
	if c.Provider == "" {
		c.Provider = "auto"
	}
	return searchReply{c.Provider, llm.Mask(c.TavilyKey), llm.Mask(c.OllamaKey), c.SearxngURL, c.WeatherPlace, search.Usage(time.Now())}
}

// searchRequest is the POST's "search" object: an absent field keeps its saved value, a key "" clears it.
type searchRequest struct {
	Provider     *string `json:"provider"`
	TavilyKey    *string `json:"tavily_key"`
	OllamaKey    *string `json:"ollama_key"`
	SearxngURL   *string `json:"searxng_url"`
	WeatherPlace *string `json:"weather_place"`
}

// config is the saved search.Config with q's fields over it.
func (q *searchRequest) config() search.Config {
	c := search.LoadConfig()
	for dst, src := range map[*string]*string{&c.Provider: q.Provider, &c.TavilyKey: q.TavilyKey, &c.OllamaKey: q.OllamaKey,
		&c.SearxngURL: q.SearxngURL, &c.WeatherPlace: q.WeatherPlace} {
		if src != nil {
			*dst = *src
		}
	}
	return c
}

type settingsRequest struct {
	Settings
	Lang string `json:"lang"`
	// LLM, OllamaURL and ClaudeKey are saved by llm.Save when any is present; absent keeps; ClaudeKey "" clears.
	LLM       map[string]llm.Profile `json:"llm"`
	OllamaURL *string                `json:"ollama_url"`
	ClaudeKey *string                `json:"claude_key"`
	Search    *searchRequest         `json:"search"`
}

func (q *settingsRequest) llmChanged() bool {
	return q.LLM != nil || q.OllamaURL != nil || q.ClaudeKey != nil
}

// llmConfig is the saved llm.Config with q's fields over it.
func (q *settingsRequest) llmConfig() llm.Config {
	c := llm.Load()
	if q.LLM != nil {
		c.Profiles = q.LLM
	}
	if q.OllamaURL != nil {
		c.OllamaURL = *q.OllamaURL
	}
	if q.ClaudeKey != nil {
		c.ClaudeKey = *q.ClaudeKey
	}
	return c
}

func forkKG() llm.KG {
	k := vars.APIConfig.Knowledge
	return llm.KG{Provider: k.Provider, Endpoint: k.Endpoint, Model: k.Model}
}

func (q *settingsRequest) valid() error {
	if !lang.Valid(q.Lang) {
		return fmt.Errorf("lang: one of %v", lang.Supported)
	}
	if q.llmChanged() {
		if err := llm.Validate(q.llmConfig()); err != nil {
			return err
		}
	}
	if q.Search != nil {
		if err := search.Validate(q.Search.config()); err != nil {
			return err
		}
	}
	return q.Validate()
}

// saveMu holds across a POST's saves, so two concurrent ones cannot end with one's language and the other's intents.
var saveMu sync.Mutex

// save writes q's settings, LLM profiles and language; a language whose intents do not load is rolled back and
// returned as intentErr (the old intents stay). err is a failed write.
func save(q *settingsRequest) (intentErr string, err error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	if err := Save(q.Settings); err != nil {
		return "", fmt.Errorf("save: %w", err)
	}
	if q.llmChanged() {
		if err := llm.Save(q.llmConfig()); err != nil {
			return "", fmt.Errorf("save llm: %w", err)
		}
	}
	if q.Search != nil {
		if err := search.Save(q.Search.config()); err != nil {
			return "", fmt.Errorf("save search: %w", err)
		}
	}
	old := lang.Current()
	if q.Lang == old {
		return "", nil
	}
	if err := lang.Save(q.Lang); err != nil {
		return "", fmt.Errorf("save lang: %w", err)
	}
	if rerr := ReloadIntents(); rerr != nil {
		intentErr = q.Lang + " intents not loaded, language stays " + old + ": " + rerr.Error()
		if err := lang.Save(old); err != nil {
			intentErr += "; rollback failed, language is " + lang.Current() + ": " + err.Error()
		}
		logger.Println("myvector voice settings: " + intentErr)
	}
	return intentErr, nil
}

// SettingsHandler serves SettingsAPI: GET reads, POST (application/json Settings + lang + optional llm, ollama_url, claude_key, search) validates,
// saves, switches the language (reloading intents; rolled back if they fail) and pushes the current language's T2.
func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	pushErr, intentErr := "", ""
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var q settingsRequest
		if !decode(w, r, &q, q.valid) {
			return
		}
		var err error
		if intentErr, err = save(&q); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := PushT2(r.Context(), q.Settings, lang.Current()); err != nil { // after a rollback, the old language's T2
			pushErr = err.Error()
			logger.Println("myvector voice settings: saved; robot not updated: " + pushErr)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	c, kg := llm.Load(), forkKG()
	profiles, url := llm.Effective(c, kg)
	defaults, _ := llm.Effective(llm.Config{LegacyETModel: c.LegacyETModel}, kg)
	json.NewEncoder(w).Encode(settingsReply{Settings: Current(), Lang: lang.Current(), Langs: lang.Supported, Voices: voices,
		SlowMin: SlowMin, SlowMax: SlowMax, LLM: profiles, Defaults: defaults, OllamaURL: url, ClaudeKey: llm.Mask(c.ClaudeKey), KGEnabled: vars.APIConfig.Knowledge.Enable,
		LLMSaved: c.Profiles != nil, Knowledge: strings.TrimSpace(kg.Provider + " " + kg.Model),
		PushError: pushErr, IntentError: intentErr, Search: currentSearch()})
}

// decode reads an application/json POST body into v and runs valid on it; on failure it has written the error.
// The JSON-only body makes a cross-site form post fail its CORS preflight.
func decode(w http.ResponseWriter, r *http.Request, v any, valid func() error) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
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
