package prompt

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Routes on the default mux the web UI serves (sdkapp/fork_voice.go).
const (
	PageURL    = "/myvector/prompt"
	API        = "/myvector/api/prompt"
	PreviewAPI = "/myvector/api/prompt/preview"
)

// maxBody fits a template at the Claude budget (2000 tokens ≈ 6000 runes, up to 4 bytes each) plus JSON.
const maxBody = 32 << 10

//go:embed prompt.html
var page []byte

// UpstreamPrompt is wire-pod's openai_prompt; vars/fork_prompt.go points it at vars.APIConfig (the page warns it is ignored).
var UpstreamPrompt = func() string { return "" }

type brainReply struct {
	Template string   `json:"template"`
	Default  string   `json:"default"`
	Saved    bool     `json:"saved"`
	Versions []string `json:"versions"`
	Appended []string `json:"appended"`
	Budget   int      `json:"budget"`
}

type reply struct {
	Brains         map[string]brainReply `json:"brains"`
	About          string                `json:"about"`
	AboutCap       int                   `json:"about_cap"`
	Tags           []string              `json:"tags"`
	Required       []string              `json:"required"`
	UpstreamIgnore bool                  `json:"upstream_prompt_ignored"`
}

func current() reply {
	s := Load()
	r := reply{Brains: map[string]brainReply{}, About: s.About, AboutCap: AboutCap, Tags: KnownTags, Required: RequiredTags,
		UpstreamIgnore: UpstreamPrompt() != ""}
	for _, b := range []string{Claude, Ollama} {
		t, saved := s.Templates[b]
		if !saved {
			t = Default(b)
		}
		_, appended := Complete(t)
		r.Brains[b] = brainReply{Template: t, Default: Default(b), Saved: saved, Versions: s.Versions[b], Appended: appended, Budget: Budget(b)}
	}
	return r
}

type apiRequest struct {
	Action   string `json:"action"` // save, reset, about, restore
	Brain    string `json:"brain"`
	Template string `json:"template"`
	About    string `json:"about"`
	Version  int    `json:"version"`
}

func (q *apiRequest) valid() error {
	if !slices.Contains([]string{"save", "reset", "about", "restore"}, q.Action) {
		return errors.New("action: save, reset, about or restore")
	}
	if q.Action != "about" && q.Brain != Claude && q.Brain != Ollama {
		return fmt.Errorf("brain: %s or %s", Claude, Ollama)
	}
	return nil
}

// APIHandler serves API: GET reads; POST application/json {action, brain, template | about | version} saves, resets,
// saves the about-me or restores a version, then replies as GET. A rejected template, about-me or version is a 400.
func APIHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var q apiRequest
		if !decode(w, r, &q, q.valid) {
			return
		}
		var err error
		switch q.Action {
		case "save":
			err = Save(q.Brain, q.Template)
		case "reset":
			err = Reset(q.Brain)
		case "about":
			err = SaveAbout(q.About)
		case "restore":
			err = Restore(q.Brain, q.Version)
		}
		if err != nil {
			code := http.StatusInternalServerError
			if errors.As(err, new(Invalid)) {
				code = http.StatusBadRequest
			}
			http.Error(w, err.Error(), code)
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(current())
}

type previewRequest struct {
	Brain    string `json:"brain"`
	Template string `json:"template"`
	About    string `json:"about"`
	Lang     string `json:"lang"`
}

func (q *previewRequest) valid() error {
	if q.Brain != Claude && q.Brain != Ollama {
		return fmt.Errorf("brain: %s or %s", Claude, Ollama)
	}
	if q.Lang == "" {
		q.Lang = lang.Current()
	} else if !lang.Valid(q.Lang) {
		return fmt.Errorf("lang: one of %v", lang.Supported)
	}
	return nil
}

type previewReply struct {
	Text     string   `json:"text"`
	Tokens   int      `json:"tokens"`
	Budget   int      `json:"budget"`
	Appended []string `json:"appended"`
	Error    string   `json:"error,omitempty"` // what Save would reject
}

// PreviewHandler serves PreviewAPI: POST application/json {brain, template, about, lang} renders the unsaved template
// with Sample data and that about-me, and reports its tokens, the brain's budget, the appended tags and any Save error.
func PreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "POST", http.StatusMethodNotAllowed)
		return
	}
	var q previewRequest
	if !decode(w, r, &q, q.valid) {
		return
	}
	full, appended := Complete(q.Template)
	v := Sample(q.Lang)
	v.User = UserBlock(q.About)
	text := Render(full, v)
	p := previewReply{Text: text, Tokens: Estimate(text), Budget: Budget(q.Brain), Appended: appended}
	if err := Validate(q.Brain, q.Template, q.About); err != nil {
		p.Error = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// PageHandler serves the editor page at PageURL.
func PageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(page)
}

// decode reads an application/json POST body into v and runs valid on it; on failure it has written the error.
// The JSON-only body makes a cross-site form post fail its CORS preflight.
func decode(w http.ResponseWriter, r *http.Request, v any, valid func() error) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
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
