package voice

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// en is English T1, T2 and slow with the Estonian pair at its defaults.
func en(t1, t2 string, slow float64) Settings {
	return Settings{t1, t2, DefaultETVoice, DefaultETVoice, slow}
}

// tempSettings isolates the voice settings and the language (en-US) and stubs PushT2 and ReloadIntents.
func tempSettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	old := SettingsPath
	SettingsPath = func() string { return path }
	t.Cleanup(func() { SettingsPath = old })
	lang.UseForTest(t, lang.English)
	stubPush(t, nil)
	stubReload(t)
	return path
}

// stubReload makes ReloadIntents record lang.Current() at each call — the language the real one would load intents for.
func stubReload(t *testing.T) *[]string {
	t.Helper()
	var seen []string
	old := ReloadIntents
	ReloadIntents = func() error { seen = append(seen, lang.Current()); return nil }
	t.Cleanup(func() { ReloadIntents = old })
	return &seen
}

// stubPush makes PushT2 record the Settings it was given, as For its language, and return err.
func stubPush(t *testing.T, err error) *[]Settings {
	t.Helper()
	var pushed []Settings
	old := PushT2
	PushT2 = func(_ context.Context, s Settings, l string) error { pushed = append(pushed, s.For(l)); return err }
	t.Cleanup(func() { PushT2 = old })
	return &pushed
}

func TestSettingsValidate(t *testing.T) {
	for _, c := range []struct {
		s  Settings
		ok bool
	}{
		{en("cori", "cori", 1.2), true},
		{en("af_bella", Stock, SlowMin), true},
		{en(Stock, "af_bella", SlowMax), true},
		{en("", "cori", 1), false},
		{en("cori", "", 1), false},
		{en("bella", "cori", 1), false},
		{en("cori", "bella", 1), false},
		{en("mari", "cori", 1), false}, // Estonian-only
		{en("cori", "mari", 1), false},
		{Settings{"cori", "cori", "af_bella", "mari", 1}, false}, // E30: the et pair is mari only
		{Settings{"cori", "cori", "mari", Stock, 1}, false},
		{Settings{"cori", "cori", "", "mari", 1}, false},
		{en("cori", "cori", 0.49), false},
		{en("cori", "cori", 2.01), false},
		{en("cori", "cori", math.NaN()), false},
	} {
		if err := c.s.Validate(); (err == nil) != c.ok {
			t.Errorf("Validate(%+v) = %v, want ok=%v", c.s, err, c.ok)
		}
	}
}

func TestSettingsCurrentDefaults(t *testing.T) {
	path := tempSettings(t)
	if got := Current(); got != (Settings{"af_bella", "af_bella", "mari", "mari", 1.2}) {
		t.Fatalf("missing file: %+v", got)
	}
	for _, bad := range []string{`{`, `{"t1":"bella","slow":1}`, `{"t1":"cori","slow":9}`, `{"t1":"cori","t2":"bella","slow":1}`} {
		os.WriteFile(path, []byte(bad), 0644)
		if got := Current(); got != Defaults() {
			t.Errorf("file %s: %+v, want defaults", bad, got)
		}
	}
	os.WriteFile(path, []byte(`{"t1":"cori"}`), 0644) // a pre-T2 file
	if got := Current(); got != (en("cori", DefaultVoice, DefaultSlow)) {
		t.Errorf("partial file: %+v", got)
	}
}

func TestSettingsSaveRoundTrip(t *testing.T) {
	tempSettings(t)
	want := Settings{Stock, "af_bella", "mari", "mari", 1.5}
	if err := Save(Settings{"cori", "cori", "mari", "mari", 1}); err != nil {
		t.Fatal(err)
	}
	if err := Save(want); err != nil { // overwrites every key
		t.Fatal(err)
	}
	if got := Current(); got != want {
		t.Fatalf("Current() = %+v, want %+v", got, want)
	}
	if err := Save(en("cori", "bella", 1)); err == nil {
		t.Fatal("Save accepted an unknown voice")
	}
	if got := Current(); got != want {
		t.Fatalf("rejected Save changed the file: %+v", got)
	}
}

func TestSettingsSaveKeepsLang(t *testing.T) {
	path := tempSettings(t)
	os.WriteFile(path, []byte(`{"lang":"et-EE","et_model":"m","t1":"cori"}`), 0644)
	if err := Save(en(Stock, "af_bella", 1.5)); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	b, _ := os.ReadFile(path)
	if json.Unmarshal(b, &got); got["lang"] != "et-EE" || got["et_model"] != "m" || got["t1"] != Stock || got["t1_et"] != "mari" {
		t.Fatalf("file = %s", b)
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
	if got.Settings != Defaults() || got.Lang != "en-US" || strings.Join(got.Langs, ",") != "en-US,et-EE" ||
		strings.Join(got.Voices["en-US"], ",") != "af_bella,cori,stock" || strings.Join(got.Voices["et-EE"], ",") != "mari" ||
		got.SlowMin != 0.5 || got.SlowMax != 2 {
		t.Fatalf("GET reply: %+v", got)
	}

	pushed := stubPush(t, nil)
	w = serve(http.MethodPost, "application/json; charset=utf-8", `{"lang":"en-US","t1":"af_bella","t2":"stock","t1_et":"mari","t2_et":"mari","slow":0.8}`)
	if w.Code != 200 || Current() != en("af_bella", Stock, 0.8) || strings.Contains(w.Body.String(), "pushError") {
		t.Fatalf("POST: %d %s, saved %+v", w.Code, w.Body, Current())
	}
	if len(*pushed) != 1 || (*pushed)[0] != Current() {
		t.Fatalf("pushed %+v, want the saved settings once", *pushed)
	}

	for _, c := range []struct {
		method, ctype, body string
		code                int
	}{
		{http.MethodPost, "application/json", `{"lang":"en-US","t1":"bella","t2":"cori","t1_et":"mari","t2_et":"mari","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"lang":"en-US","t1":"cori","t2":"bella","t1_et":"mari","t2_et":"mari","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"lang":"en-US","t1":"cori","t2":"cori","t1_et":"mari","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"t1":"cori","t2":"cori","t1_et":"mari","t2_et":"mari","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"lang":"fi-FI","t1":"cori","t2":"cori","t1_et":"mari","t2_et":"mari","slow":1}`, 400},
		{http.MethodPost, "application/json", `{"t1":"cori","slow":1,"x":1}`, 400},
		{http.MethodPost, "application/json", `{"t1":"cori","slow":"1"}`, 400},
		{http.MethodPost, "text/plain", `{"t1":"cori","slow":1}`, 415},
		{http.MethodPut, "application/json", `{"t1":"cori","slow":1}`, 405},
	} {
		if w := serve(c.method, c.ctype, c.body); w.Code != c.code {
			t.Errorf("%s %s %s: %d, want %d", c.method, c.ctype, c.body, w.Code, c.code)
		}
	}
	if Current() != en("af_bella", Stock, 0.8) || len(*pushed) != 1 || lang.Current() != lang.English {
		t.Fatalf("a rejected request changed the settings to %+v or pushed: %+v", Current(), *pushed)
	}
}

func TestSettingsLangSwitchReloadsIntents(t *testing.T) {
	tempSettings(t)
	reloads, pushed := stubReload(t), stubPush(t, nil)
	body := `{"lang":"et-EE","t1":"cori","t2":"af_bella","t1_et":"mari","t2_et":"mari","slow":1}`
	// reloads is the language current at each reload: the new one, so the reload follows lang.Save
	if w := serve(http.MethodPost, "application/json", body); w.Code != 200 || lang.Current() != lang.Estonian ||
		strings.Join(*reloads, ",") != "et-EE" {
		t.Fatalf("switch to et-EE: %d %s, lang %s, reloads saw %v; want et-EE, 1 reload seeing et-EE", w.Code, w.Body, lang.Current(), *reloads)
	}
	if len(*pushed) != 1 || (*pushed)[0].T2 != "mari" {
		t.Fatalf("pushed %+v, want the et-EE T2 mari", *pushed)
	}
	if w := serve(http.MethodPost, "application/json", body); w.Code != 200 || len(*reloads) != 1 {
		t.Fatalf("same lang again: %d, reloads saw %v; want no reload", w.Code, *reloads)
	}
	if w := serve(http.MethodPost, "application/json", strings.Replace(body, "et-EE", "en-US", 1)); w.Code != 200 ||
		lang.Current() != lang.English || strings.Join(*reloads, ",") != "et-EE,en-US" || (*pushed)[2].T2 != "af_bella" {
		t.Fatalf("back to en-US: %d, lang %s, reloads saw %v, pushed %+v; want reloads et-EE,en-US", w.Code, lang.Current(), *reloads, *pushed)
	}
}

func TestSettingsLangSwitchRollsBackOnIntentError(t *testing.T) {
	tempSettings(t)
	var reloads []string
	fail := errors.New("open ./intent-data/et-EE.json: permission denied")
	ReloadIntents = func() error { reloads = append(reloads, lang.Current()); return fail } // tempSettings' cleanup restores it
	pushed := stubPush(t, nil)
	body := `{"lang":"et-EE","t1":"cori","t2":"af_bella","t1_et":"mari","t2_et":"mari","slow":1}`
	w := serve(http.MethodPost, "application/json", body)
	var got settingsReply
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || !strings.Contains(got.IntentError, fail.Error()) || got.Lang != lang.English || lang.Current() != lang.English {
		t.Fatalf("failed reload: %d %s, lang %s; want 200 with intentError and en-US kept", w.Code, w.Body, lang.Current())
	}
	if b, _ := os.ReadFile(lang.Path()); !strings.Contains(string(b), `"lang": "en-US"`) { // the saved one, not just the cache
		t.Fatalf("file after rollback = %s, want lang en-US", b)
	}
	if Current() != en("cori", "af_bella", 1) || len(*pushed) != 1 || (*pushed)[0].T2 != "af_bella" {
		t.Fatalf("saved %+v, pushed %+v; want the voices saved and the en-US T2 pushed", Current(), *pushed)
	}
	ReloadIntents = func() error { reloads = append(reloads, lang.Current()); return nil }
	if w := serve(http.MethodPost, "application/json", body); w.Code != 200 || lang.Current() != lang.Estonian ||
		strings.Join(reloads, ",") != "et-EE,et-EE" || strings.Contains(w.Body.String(), "intentError") {
		t.Fatalf("re-POST: %d %s, lang %s, reloads saw %v; want the switch retried and kept", w.Code, w.Body, lang.Current(), reloads)
	}
}

// Two concurrent language switches must end with the intents of the language that stands (saveMu).
func TestSettingsLangSwitchConcurrent(t *testing.T) {
	tempSettings(t)
	var mu sync.Mutex
	loaded := lang.English
	ReloadIntents = func() error { // as reloadIntents: read the language, load (slowly), then store
		l := lang.Current()
		time.Sleep(time.Duration(rand.Intn(2000)) * time.Microsecond) // so a later reload can finish first
		mu.Lock()
		loaded = l
		mu.Unlock()
		return nil
	}
	PushT2 = func(context.Context, Settings, string) error { return nil }
	for i := 0; i < 30; i++ {
		var wg sync.WaitGroup
		for _, l := range []string{lang.Estonian, lang.English} {
			wg.Add(1)
			go func(l string) {
				defer wg.Done()
				serve(http.MethodPost, "application/json", `{"lang":"`+l+`","t1":"cori","t2":"cori","t1_et":"mari","t2_et":"mari","slow":1}`)
			}(l)
		}
		wg.Wait()
		if mu.Lock(); loaded != lang.Current() {
			t.Fatalf("round %d: lang %s with %s intents", i, lang.Current(), loaded)
		}
		mu.Unlock()
	}
}

func TestSettingsFor(t *testing.T) {
	s := Settings{"cori", Stock, "mari", "mari", 1.3}
	if got := s.For(lang.Estonian); got != (Settings{"mari", "mari", "mari", "mari", 1.3}) {
		t.Errorf("For(et-EE) = %+v", got)
	}
	if got := s.For(lang.English); got != s {
		t.Errorf("For(en-US) = %+v, want unchanged", got)
	}
}

func TestSettingsHandlerPushFailKeepsSave(t *testing.T) {
	tempSettings(t)
	stubPush(t, errors.New("SETUP: no ROBOT_TTS_URL"))
	w := serve(http.MethodPost, "application/json", `{"lang":"en-US","t1":"cori","t2":"af_bella","t1_et":"mari","t2_et":"mari","slow":1.1}`)
	var got settingsReply
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.PushError != "SETUP: no ROBOT_TTS_URL" || Current() != en("cori", "af_bella", 1.1) {
		t.Fatalf("POST: %d %s, saved %+v; want 200 with pushError and the save kept", w.Code, w.Body, Current())
	}
}

// fakeScript makes pushScript a shell script with body; it gets voice-push.sh's args.
func fakeScript(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "voice-push.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	old := pushScript
	pushScript = func() (string, error) { return p, nil }
	t.Cleanup(func() { pushScript = old })
}

func TestPushT2Args(t *testing.T) {
	args := filepath.Join(t.TempDir(), "args")
	fakeScript(t, `echo "$@" >`+args)
	s := Settings{"cori", "af_bella", "mari", "mari", 1.25}
	for l, want := range map[string]string{lang.English: "af_bella 1.25 en-US\n", lang.Estonian: "mari 1.25 et-EE\n"} {
		if err := pushT2(context.Background(), s, l); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(args); string(b) != want {
			t.Fatalf("%s: voice-push.sh args %q, want %q (the language's T2, slow, lang)", l, b, want)
		}
	}
}

func TestPushT2ErrorHasLastLine(t *testing.T) {
	fakeScript(t, `echo checking; echo "SETUP: no ROBOT_TTS_URL" >&2; exit 2`)
	err := pushT2(context.Background(), Defaults(), lang.English)
	if err == nil || !strings.HasSuffix(err.Error(), ": SETUP: no ROBOT_TTS_URL") {
		t.Fatalf("err = %v, want the script's last line", err)
	}
}

func TestPushT2Timeout(t *testing.T) {
	old := PushTimeout
	PushTimeout = 100 * time.Millisecond
	t.Cleanup(func() { PushTimeout = old })
	fakeScript(t, `sleep 30 & wait`) // the orphaned sleep keeps the output pipe open, as a hung ssh would
	start := time.Now()
	err := pushT2(context.Background(), Defaults(), lang.English)
	if err == nil || !strings.Contains(err.Error(), "no answer") || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v after %v, want a timeout within 5 s", err, time.Since(start))
	}
}

func TestPushScriptFound(t *testing.T) {
	p, err := pushScript()
	if err != nil || !strings.HasSuffix(p, "/robot/firmware/voice-push.sh") {
		t.Fatalf("pushScript() = %q, %v", p, err)
	}
}

func TestSettingsPage(t *testing.T) {
	w := httptest.NewRecorder()
	PageHandler(w, httptest.NewRequest(http.MethodGet, SettingsPage, nil))
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), `"`+SettingsAPI+`"`) {
		t.Fatalf("page: %s, API path %s missing", w.Header().Get("Content-Type"), SettingsAPI)
	}
	// the page picks each language's LLM from both providers and shows a rolled-back switch
	for _, want := range []string{`<optgroup label="Local (Ollama)"`, `<optgroup label="Claude API"`, `"` + llm.ModelsAPI + `"`, `"` + llm.TestAPI + `"`,
		`claude_key = ""`, `!cur.llmSaved`, `if (s.intentError)`, `+ s.intentError`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(w.Body.String(), `id="et_model"`) {
		t.Error("page still has the et_model field")
	}
}

// Every language's voices (minus Stock) must be the TTS service's voices, or a pick would 400 there.
func TestSettingsVoicesMatchService(t *testing.T) {
	src, err := os.ReadFile("../../../../tts/server.py")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)\nVOICES = \{\n(.*?)\n\}`).FindSubmatch(src)
	if m == nil {
		t.Fatal("no VOICES dict in tts/server.py")
	}
	var service, ours []string
	for _, k := range regexp.MustCompile(`(?m)^\s*"([^"]+)": \(`).FindAllSubmatch(m[1], -1) {
		service = append(service, string(k[1]))
	}
	for _, l := range lang.Supported {
		for _, v := range VoicesFor(l) {
			if v != Stock {
				ours = append(ours, v)
			}
		}
	}
	sort.Strings(service)
	sort.Strings(ours)
	if strings.Join(ours, ",") != strings.Join(service, ",") {
		t.Errorf("voices minus stock %v, tts/server.py VOICES %v", ours, service)
	}
}

func demo(t *testing.T, ctype, body string, robotErr error) (*httptest.ResponseRecorder, *robot) {
	t.Helper()
	return demoOn(t, &robot{needControl: true, grant: true}, ctype, body, robotErr)
}

func demoOn(t *testing.T, rb *robot, ctype, body string, robotErr error) (*httptest.ResponseRecorder, *robot) {
	t.Helper()
	service(t, func(w http.ResponseWriter, r *http.Request) { t.Error("service called for stock") })
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
	if rb.controlled || rb.released != 1 {
		t.Errorf("behavior control held %v, released %d times; want released once", rb.controlled, rb.released)
	}
}

func TestDemoHandlerSpeaksLanguageT1(t *testing.T) {
	tempSettings(t)
	lang.UseForTest(t, lang.Estonian)
	var asked []any
	service(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		asked = append(asked, req["voice"])
		w.Write(make([]byte, 100))
	})
	for _, c := range []struct{ body, voice string }{
		{`{"t1":"stock","slow":1,"text":"tere"}`, "mari"}, // lang omitted: the current et-EE, t1_et defaulted
		{`{"lang":"en-US","t1":"stock","t1_et":"mari","slow":1,"text":"hi"}`, "stock"},
	} {
		rb := &robot{needControl: true, grant: true, verdict: make(chan *vectorpb.ExternalAudioStreamResponse, 1), completeOnEnd: true}
		h := DemoHandler(func(*http.Request) (context.Context, *vector.Vector, error) {
			return context.Background(), &vector.Vector{Conn: rb}, nil
		})
		r := httptest.NewRequest(http.MethodPost, DemoAPI, strings.NewReader(c.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"voice":"`+c.voice+`"}` {
			t.Errorf("%s: %d %s, want %s", c.body, w.Code, w.Body, c.voice)
		}
	}
	if len(asked) != 1 || asked[0] != "mari" {
		t.Errorf("service asked for %v, want mari once", asked)
	}
}

func TestDemoHandlerControlNotGranted(t *testing.T) {
	old := ControlWait
	ControlWait = 50 * time.Millisecond
	t.Cleanup(func() { ControlWait = old })
	w, rb := demoOn(t, &robot{needControl: true}, "application/json", `{"t1":"stock","slow":1,"text":"hi"}`, nil)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "not granted") || len(rb.said) != 0 {
		t.Errorf("POST: %d %q with %d SayText calls; want 502 not granted, none", w.Code, w.Body, len(rb.said))
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
		{"application/json", `{"t1":"stock","slow":1,"text":"hi","lang":"fi-FI"}`, nil, 400},
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

func TestSettingsLLM(t *testing.T) {
	tempSettings(t)
	old := vars.APIConfig.Knowledge
	t.Cleanup(func() { vars.APIConfig.Knowledge = old })
	vars.APIConfig.Knowledge.Provider, vars.APIConfig.Knowledge.Endpoint, vars.APIConfig.Knowledge.Model = "custom", "http://localhost:11434/v1", "gemma4:e4b"
	vars.APIConfig.Knowledge.Enable = true
	const key = "sk-ant-0123456789abcdefWxYz"
	get := func() (settingsReply, string) {
		t.Helper()
		var got settingsReply
		w := serve(http.MethodGet, "", "")
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("GET: %d %s", w.Code, w.Body)
		}
		return got, w.Body.String()
	}
	got, _ := get()
	if got.LLM[lang.English] != (llm.Profile{Provider: llm.Ollama, Model: "gemma4:e4b"}) || got.Defaults[lang.Estonian].Model != llm.DefaultETModel ||
		got.OllamaURL != "http://localhost:11434" || got.ClaudeKey != "" || !got.KGEnabled || got.LLMSaved || got.Knowledge != "custom gemma4:e4b" {
		t.Fatalf("GET defaults: %+v", got)
	}
	post := func(extra string) *httptest.ResponseRecorder {
		return serve(http.MethodPost, "application/json", `{"lang":"en-US","t1":"cori","t2":"cori","t1_et":"mari","t2_et":"mari","slow":1`+extra+`}`)
	}
	if w := post(`,"claude_key":"` + key + `","llm":{"en-US":{"provider":"ollama","model":"qwen3.5:4b"},"et-EE":{"provider":"claude","model":"claude-haiku-5-5"}}`); w.Code != 200 {
		t.Fatalf("POST claude profile: %d %s", w.Code, w.Body)
	}
	got, body := get()
	if strings.Contains(body, key) || got.ClaudeKey != "sk-ant-…WxYz" || !got.LLMSaved || got.LLM[lang.Estonian] != (llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}) {
		t.Fatalf("GET after save: key shown %v, %+v", strings.Contains(body, key), got)
	}
	if got.Defaults[lang.Estonian] != (llm.Profile{Provider: llm.Ollama, Model: llm.DefaultETModel}) {
		t.Fatalf("defaults follow the saved profile: %+v", got.Defaults)
	}
	if w := post(``); w.Code != 200 || llm.Load().ClaudeKey != key {
		t.Fatalf("POST without llm fields: %d, key %q kept?", w.Code, llm.Load().ClaudeKey)
	}
	before, _ := os.ReadFile(lang.Path())
	if w := post(`,"claude_key":""`); w.Code != 400 { // et-EE still uses Claude
		t.Fatalf("clearing the key under a Claude profile: %d, want 400", w.Code)
	}
	if after, _ := os.ReadFile(lang.Path()); string(after) != string(before) {
		t.Fatalf("a rejected POST changed the file:\n%s", after)
	}
	if w := post(`,"claude_key":"","llm":{"et-EE":{"provider":"ollama","model":"estllm:x"}}`); w.Code != 200 || llm.Load().ClaudeKey != "" {
		t.Fatalf("clear key with the profile moved off Claude: %d %s", w.Code, w.Body)
	}
}
