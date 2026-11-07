package prompt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	if w := post(t, APIHandler, `{"action":"about","about":"`+strings.Repeat("a", AboutCap+1)+`"}`); w.Code != http.StatusBadRequest {
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

func TestPromptAPISaveRoundTrip(t *testing.T) {
	_, _ = useStore(t)
	old := UpstreamPrompt
	UpstreamPrompt = func() string { return "You are a pirate." }
	t.Cleanup(func() { UpstreamPrompt = old })
	post(t, APIHandler, `{"action":"about","about":"Mari"}`)
	w := post(t, APIHandler, `{"action":"save","brain":"claude","template":"Be brief.\n\n{user}"}`)
	var r reply
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != http.StatusOK {
		t.Fatalf("save: %d %v %q", w.Code, err, w.Body.String())
	}
	c, o := r.Brains[Claude], r.Brains[Ollama]
	if !c.Saved || c.Template != "Be brief.\n\n{user}" || len(c.Versions) != 1 || strings.Join(c.Appended, " ") != strings.Join(RequiredTags, " ") {
		t.Errorf("claude after save: %+v", c)
	}
	if o.Saved || o.Template != Default(Ollama) || o.Budget != Budget(Ollama) || r.About != "Mari" || !r.UpstreamIgnore {
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
	for _, want := range []string{API + `"`, PreviewAPI + `"`, `id="tmpl"`, `id="about"`, `id="versions"`, `id="meter"`, "--accent:"} {
		if !strings.Contains(b, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}
