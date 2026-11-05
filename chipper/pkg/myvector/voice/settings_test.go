package voice

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
)

func tempSettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	old := SettingsPath
	SettingsPath = func() string { return path }
	t.Cleanup(func() { SettingsPath = old })
	return path
}

func TestSettingsValidate(t *testing.T) {
	for _, c := range []struct {
		s  Settings
		ok bool
	}{
		{Settings{"cori", 1.2}, true},
		{Settings{"af_bella", SlowMin}, true},
		{Settings{Stock, SlowMax}, true},
		{Settings{"", 1}, false},
		{Settings{"bella", 1}, false},
		{Settings{"cori", 0.49}, false},
		{Settings{"cori", 2.01}, false},
		{Settings{"cori", math.NaN()}, false},
	} {
		if err := c.s.Validate(); (err == nil) != c.ok {
			t.Errorf("Validate(%+v) = %v, want ok=%v", c.s, err, c.ok)
		}
	}
}

func TestSettingsCurrentDefaults(t *testing.T) {
	path := tempSettings(t)
	if got := Current(); got != (Settings{"cori", 1.2}) {
		t.Fatalf("missing file: %+v", got)
	}
	for _, bad := range []string{`{`, `{"t1":"bella","slow":1}`, `{"t1":"cori","slow":9}`} {
		os.WriteFile(path, []byte(bad), 0644)
		if got := Current(); got != Defaults() {
			t.Errorf("file %s: %+v, want defaults", bad, got)
		}
	}
	os.WriteFile(path, []byte(`{"t1":"af_bella"}`), 0644)
	if got := Current(); got != (Settings{"af_bella", DefaultSlow}) {
		t.Errorf("partial file: %+v", got)
	}
}

func TestSettingsSaveRoundTrip(t *testing.T) {
	tempSettings(t)
	want := Settings{Stock, 1.5}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got := Current(); got != want {
		t.Fatalf("Current() = %+v, want %+v", got, want)
	}
	if err := Save(Settings{"bella", 1}); err == nil {
		t.Fatal("Save accepted an unknown voice")
	}
	if got := Current(); got != want {
		t.Fatalf("rejected Save changed the file: %+v", got)
	}
}

func TestSettingsSaveFailLeavesNoTemp(t *testing.T) {
	path := tempSettings(t)
	os.Mkdir(path, 0755) // rename onto a directory fails
	if err := Save(Defaults()); err == nil {
		t.Fatal("Save over a directory succeeded")
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".myvectorConfig-*")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}

func serve(method, ctype, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, SettingsAPI, strings.NewReader(body))
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	w := httptest.NewRecorder()
	SettingsHandler(w, r)
	return w
}

func TestSettingsHandler(t *testing.T) {
	tempSettings(t)
	w := serve(http.MethodGet, "", "")
	var got settingsReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	if got.Settings != Defaults() || strings.Join(got.Voices, ",") != "cori,af_bella,stock" || got.SlowMin != 0.5 || got.SlowMax != 2 {
		t.Fatalf("GET reply: %+v", got)
	}

	w = serve(http.MethodPost, "application/json; charset=utf-8", `{"t1":"af_bella","slow":0.8}`)
	if w.Code != 200 || Current() != (Settings{"af_bella", 0.8}) {
		t.Fatalf("POST: %d %s, saved %+v", w.Code, w.Body, Current())
	}

	for _, c := range []struct {
		method, ctype, body string
		code                int
	}{
		{http.MethodPost, "application/json", `{"t1":"bella","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"t1":"cori","slow":1,"x":1}`, 400},
		{http.MethodPost, "application/json", `{"t1":"cori","slow":"1"}`, 400},
		{http.MethodPost, "text/plain", `{"t1":"cori","slow":1}`, 415},
		{http.MethodPut, "application/json", `{"t1":"cori","slow":1}`, 405},
	} {
		if w := serve(c.method, c.ctype, c.body); w.Code != c.code {
			t.Errorf("%s %s %s: %d, want %d", c.method, c.ctype, c.body, w.Code, c.code)
		}
	}
	if Current() != (Settings{"af_bella", 0.8}) {
		t.Fatalf("a rejected request changed the settings: %+v", Current())
	}
}

func TestSettingsPage(t *testing.T) {
	w := httptest.NewRecorder()
	PageHandler(w, httptest.NewRequest(http.MethodGet, SettingsPage, nil))
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), `"`+SettingsAPI+`"`) {
		t.Fatalf("page: %s, API path %s missing", w.Header().Get("Content-Type"), SettingsAPI)
	}
}

// Voices (minus Stock) must be the TTS service's voices, or a pick would 400 there.
func TestSettingsVoicesMatchService(t *testing.T) {
	src, err := os.ReadFile("../../../../tts/server.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range Voices {
		if in := strings.Contains(string(src), `"`+v+`": (`); in == (v == Stock) {
			t.Errorf("voice %q: in tts/server.py VOICES = %v", v, in)
		}
	}
}

func demo(t *testing.T, ctype, body string, robotErr error) (*httptest.ResponseRecorder, *robot) {
	t.Helper()
	rb := &robot{}
	h := DemoHandler(func(*http.Request) (context.Context, *vector.Vector, error) {
		return context.Background(), &vector.Vector{Conn: rb}, robotErr
	})
	r := httptest.NewRequest(http.MethodPost, DemoAPI, strings.NewReader(body))
	r.Header.Set("Content-Type", ctype)
	w := httptest.NewRecorder()
	h(w, r)
	return w, rb
}

func TestDemoHandlerSpeaksUnsavedSelection(t *testing.T) {
	path := tempSettings(t)
	w, rb := demo(t, "application/json", `{"t1":"stock","slow":1.5,"text":"hello there"}`, nil)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"voice":"stock"}` {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	if len(rb.said) != 1 || rb.said[0].Text != "hello there" || rb.said[0].DurationScalar != 1.5 {
		t.Errorf("SayText = %v, want the posted text at slow 1.5", rb.said)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("demo saved the settings: %v", err)
	}
}

func TestDemoHandlerRejects(t *testing.T) {
	for _, c := range []struct {
		ctype, body string
		robotErr    error
		code        int
	}{
		{"application/json", `{"t1":"stock","slow":1,"text":""}`, nil, 400},
		{"application/json", `{"t1":"stock","slow":1,"text":"` + strings.Repeat("é", DemoMaxText+1) + `"}`, nil, 400},
		{"application/json", `{"t1":"bella","slow":1,"text":"hi"}`, nil, 400},
		{"application/json", `{"t1":"stock","slow":1,"text":"hi","x":1}`, nil, 400},
		{"text/plain", `{"t1":"stock","slow":1,"text":"hi"}`, nil, 415},
		{"application/json", `{"t1":"stock","slow":1,"text":"hi"}`, errors.New("no robot"), 502},
	} {
		w, rb := demo(t, c.ctype, c.body, c.robotErr)
		if w.Code != c.code || len(rb.said) != 0 {
			t.Errorf("%s %.40s: %d with %d SayText calls, want %d and none", c.ctype, c.body, w.Code, len(rb.said), c.code)
		}
	}
	w := httptest.NewRecorder()
	DemoHandler(nil)(w, httptest.NewRequest(http.MethodGet, DemoAPI, nil))
	if w.Code != 405 {
		t.Errorf("GET: %d, want 405", w.Code)
	}
}
