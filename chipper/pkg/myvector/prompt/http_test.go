package prompt

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func post(t *testing.T, h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, API, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestPromptAPIRejectsUnknownTag(t *testing.T) {
	_, _ = useStore(t)
	w := post(t, APIHandler, `{"action":"save","brain":"ollama","template":"Hi {weather}"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "{weather}") {
		t.Errorf("unknown tag: %d %q", w.Code, w.Body.String())
	}
	if w := post(t, APIHandler, `{"action":"save","brain":"claude","about":"`+strings.Repeat("a", AboutCap+1)+`"}`); w.Code != http.StatusBadRequest {
		t.Errorf("over-cap about: %d", w.Code)
	}
	if w := post(t, APIHandler, `{"action":"restore","brain":"claude","version":0}`); w.Code != http.StatusBadRequest {
		t.Errorf("restore with no versions: %d", w.Code)
	}
	if w := post(t, APIHandler, `{"action":"save","brain":"gpt","template":"x"}`); w.Code != http.StatusBadRequest {
		t.Errorf("unknown brain: %d", w.Code)
	}
	if _, ok := Load().Templates[Ollama]; ok {
		t.Error("a rejected template was saved")
	}
}

func TestPromptAPIRejectsNonJSON(t *testing.T) {
	path, _ := useStore(t)
	req := httptest.NewRequest(http.MethodPost, API, strings.NewReader(`{"action":"save","brain":"claude","about":"Mari"}`))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	APIHandler(w, req)
	if _, err := os.Stat(path); w.Code != http.StatusUnsupportedMediaType || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("text/plain post: %d, file stat err %v (want 415, nothing written)", w.Code, err)
	}
}

func TestPromptAPIWriteFailureIs500(t *testing.T) {
	_, _ = useStore(t)
	dir := filepath.Join(t.TempDir(), "missing")
	Path = func() string { return filepath.Join(dir, "myvectorPrompt.json") }
	if w := post(t, APIHandler, `{"action":"save","brain":"claude","about":"Mari"}`); w.Code != http.StatusInternalServerError {
		t.Errorf("unwritable store: %d %q, want 500", w.Code, w.Body.String())
	}
}

func TestPromptAPICorruptStore(t *testing.T) {
	path, _ := useStore(t)
	os.WriteFile(path, []byte("{not json"), 0o644)
	w := post(t, APIHandler, `{"action":"save","brain":"claude","template":"Hi."}`)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("save over a corrupt store: %d, want 500", w.Code)
	}
	get := httptest.NewRecorder()
	APIHandler(get, httptest.NewRequest(http.MethodGet, API, nil))
	var r reply
	if json.Unmarshal(get.Body.Bytes(), &r); !strings.Contains(r.LoadError, "fix or delete it") || r.Brains[Claude].Template != Default(Claude) {
		t.Errorf("GET over a corrupt store: load_error %q", r.LoadError)
	}
}

func TestPromptAPISaveRoundTrip(t *testing.T) {
	_, _ = useStore(t)
	old := UpstreamPrompt
	UpstreamPrompt = func() string { return "You are a pirate." }
	t.Cleanup(func() { UpstreamPrompt = old })
	post(t, APIHandler, `{"action":"save","brain":"claude","about":"Mari"}`)
	w := post(t, APIHandler, `{"action":"save","brain":"claude","template":"Be brief.\n\n{user}"}`)
	var r reply
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != http.StatusOK {
		t.Fatalf("save: %d %v %q", w.Code, err, w.Body.String())
	}
	c, o := r.Brains[Claude], r.Brains[Ollama]
	if !c.Saved || c.Template != "Be brief.\n\n{user}" || len(c.Versions) != 1 || strings.Join(c.Appended, " ") != strings.Join(RequiredTags, " ") {
		t.Errorf("claude after save: %+v", c)
	}
	if o.Saved || o.Template != Default(Ollama) || o.Budget != Budget(Ollama) || r.About != "Mari" || !r.UpstreamIgnore || r.LoadError != "" {
		t.Errorf("reply: ollama %+v, about %q, upstream ignored %v", o, r.About, r.UpstreamIgnore)
	}
	get := httptest.NewRecorder()
	APIHandler(get, httptest.NewRequest(http.MethodGet, API, nil))
	if strings.TrimSpace(get.Body.String()) != strings.TrimSpace(w.Body.String()) {
		t.Errorf("GET differs from the POST reply:\n%s\n%s", get.Body.String(), w.Body.String())
	}
	w = post(t, APIHandler, `{"action":"reset","brain":"claude"}`)
	json.Unmarshal(w.Body.Bytes(), &r)
	if r.Brains[Claude].Saved || r.Brains[Claude].Template != Default(Claude) {
		t.Errorf("after reset: %+v", r.Brains[Claude])
	}
}

// One Save saves the template and the about-me together, validated as a pair: this about-me only fits beside the
// new, shorter template, so saving it first (or alone) would be rejected. A rejected pair writes neither.
func TestPromptAPISaveBoth(t *testing.T) {
	_, _ = useStore(t)
	long := strings.Repeat("a", AboutCap)
	full := Default(Ollama) + "\n"
	for Validate(Ollama, full+"x", "") == nil { // pad to the budget's edge
		full += "x"
	}
	if err := Validate(Ollama, full, ""); err != nil {
		t.Fatalf("setup: padded template rejected alone: %v", err)
	}
	if Validate(Ollama, full, long) == nil {
		t.Fatal("setup: padded template still fits the long about-me")
	}
	if err := Save(Ollama, &full, nil); err != nil {
		t.Fatal(err)
	}
	if w := post(t, APIHandler, `{"action":"save","brain":"ollama","about":"`+long+`"}`); w.Code != http.StatusBadRequest {
		t.Errorf("about-me alone over the saved template's budget: %d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"action": "save", "brain": Ollama, "template": "Be brief.\n\n{user}", "about": long})
	w := post(t, APIHandler, string(body))
	var r reply
	if json.Unmarshal(w.Body.Bytes(), &r); w.Code != http.StatusOK || r.About != long || r.Brains[Ollama].Template != "Be brief.\n\n{user}" {
		t.Fatalf("save both: %d about %d runes, template %q", w.Code, len(r.About), r.Brains[Ollama].Template)
	}
	body, _ = json.Marshal(map[string]string{"action": "save", "brain": Ollama, "template": "Hi {weather}", "about": "Mari"})
	if w := post(t, APIHandler, string(body)); w.Code != http.StatusBadRequest {
		t.Errorf("bad template with a good about-me: %d", w.Code)
	}
	if s := Load(); s.About != long || s.Templates[Ollama] != "Be brief.\n\n{user}" {
		t.Errorf("a rejected pair wrote: about %q, template %q", s.About, s.Templates[Ollama])
	}
	if w := post(t, APIHandler, `{"action":"save","brain":"claude","about":""}`); w.Code != http.StatusOK || Load().About != "" || Load().Templates[Claude] != "" {
		t.Errorf("about-me alone: %d, cleared %q, claude template pinned %q", w.Code, Load().About, Load().Templates[Claude])
	}
}

func TestPreviewReportsAppended(t *testing.T) {
	_, _ = useStore(t)
	w := post(t, PreviewHandler, `{"brain":"ollama","template":"Be brief.\n\n{user}","about":"Mari","lang":"et-EE"}`)
	var p previewReply
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || w.Code != http.StatusOK {
		t.Fatalf("preview: %d %v %q", w.Code, err, w.Body.String())
	}
	if strings.Join(p.Appended, " ") != strings.Join(RequiredTags, " ") || p.Budget != Budget(Ollama) || p.Tokens != Estimate(p.Text) || p.Error != "" {
		t.Errorf("preview: %+v", p)
	}
	if !strings.HasPrefix(p.Text, "Be brief.\n\nAbout the person you are talking with:\nMari") || !strings.HasSuffix(p.Text, LangLine(lang.Estonian)) {
		t.Errorf("preview text: %q", p.Text)
	}
	w = post(t, PreviewHandler, `{"brain":"ollama","template":"`+strings.Repeat("x", 3*Budget(Ollama))+`"}`)
	json.Unmarshal(w.Body.Bytes(), &p)
	if !strings.Contains(p.Error, "budget") {
		t.Errorf("over-budget preview error = %q", p.Error)
	}
}

func TestPromptPageServed(t *testing.T) {
	w := httptest.NewRecorder()
	PageHandler(w, httptest.NewRequest(http.MethodGet, PageURL, nil))
	b := w.Body.String()
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("page: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, want := range []string{API + `"`, PreviewAPI + `"`, `id="tmpl"`, `id="about"`, `id="versions"`, `id="meter"`, "--accent:", "if (cur.load_error) notes.push("} {
		if !strings.Contains(b, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if n := strings.Count(b, ">Save</button>"); n != 1 || strings.Contains(b, "saveAbout") || !strings.Contains(b, "body.about = ") {
		t.Errorf("want one Save button that also sends the about-me: %d Save buttons", n)
	}
	if strings.Contains(b, `$("warn").innerHTML`) {
		t.Error("notices built with innerHTML: server text must go in via textContent")
	}
}
