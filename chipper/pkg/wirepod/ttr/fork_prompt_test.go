package wirepod_ttr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

func useKnowledge(t *testing.T, provider, endpoint, model string) {
	t.Helper()
	old := vars.APIConfig.Knowledge
	t.Cleanup(func() { vars.APIConfig.Knowledge = old })
	vars.APIConfig.Knowledge.Provider = provider
	vars.APIConfig.Knowledge.Endpoint = endpoint
	vars.APIConfig.Knowledge.Model = model
	vars.APIConfig.Knowledge.OpenAIPrompt = ""
	vars.APIConfig.Knowledge.CommandsEnable = false
	vars.APIConfig.Knowledge.SaveChat = false
}

const ollamaEndpoint = "http://localhost:11434/v1"

// useBotClock pins forkNow to Saturday 2026-11-07 07:43 UTC and gives robot "esn" the zone in its RobotSettings jdoc.
func useBotClock(t *testing.T, zone string) {
	t.Helper()
	oldNow, oldDocs, oldPath := forkNow, vars.BotJdocs, vars.JdocsPath
	t.Cleanup(func() { forkNow, vars.BotJdocs, vars.JdocsPath = oldNow, oldDocs, oldPath })
	forkNow = func() time.Time { return time.Date(2026, 11, 7, 7, 43, 0, 0, time.UTC) }
	vars.BotJdocs, vars.JdocsPath = nil, filepath.Join(t.TempDir(), "jdocs.json")
	vars.AddJdoc("vic:esn", "vic.RobotSettings", vars.AJdoc{JsonDoc: `{"time_zone": "` + zone + `"}`})
}

func TestForkPromptEstonian(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	useBotClock(t, "Europe/Tallinn")
	r := CreateAIReq("mis nädalapäev täna on?", "esn", false, true)
	if r.Model != llm.DefaultETModel {
		t.Errorf("Model = %q, want %q", r.Model, llm.DefaultETModel)
	}
	if want := "\n\nPraegu on laupäev, 7. november 2026, kell 9:43 (Europe/Tallinn).\n\nAnswer in Estonian. Keep names, brands and titles as written."; !strings.HasSuffix(r.Messages[0].Content, want) {
		t.Errorf("system prompt lacks the now + language lines: %q", r.Messages[0].Content)
	}
	if r.ReasoningEffort != "none" {
		t.Errorf("ReasoningEffort = %q", r.ReasoningEffort)
	}
}

func TestForkPromptEnglishNow(t *testing.T) {
	lang.UseForTest(t, lang.English)
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	useBotClock(t, "Asia/Tokyo")
	vars.APIConfig.Knowledge.OpenAIPrompt = "You are a pirate."
	r := CreateAIReq("what day is it", "esn", false, true)
	want := prompt.Render(prompt.Default(prompt.Ollama), prompt.Vars{
		Now: "It is Saturday, 7 November 2026, 16:43 (Asia/Tokyo).", Speech: prompt.SpeechBlock(),
	})
	if r.Model != "qwen3.5:4b" || r.Messages[0].Content != want {
		t.Errorf("English request: model %q, prompt %q", r.Model, r.Messages[0].Content)
	}
	if c := r.Messages[0].Content; strings.Contains(c, "pirate") || strings.Contains(c, "concise yet informative") || strings.Contains(c, "Answer in") {
		t.Errorf("upstream prompt or a language line survived: %q", c)
	}
	if r.ReasoningEffort != "none" {
		t.Errorf("Ollama ReasoningEffort = %q, want none", r.ReasoningEffort)
	}
}

// Claude and Gemini presets are "custom" too (scripts/llm.sh): only the Ollama port gets the ET model + reasoning.
func TestForkPromptOpenAIOmitsReasoning(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	for _, k := range [][3]string{
		{"openai", "", "gpt-4o-mini"},
		{"custom", "https://api.anthropic.com/v1", "claude-haiku-5-5"},
		{"custom", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-flash"},
	} {
		useKnowledge(t, k[0], k[1], k[2])
		r := CreateAIReq("mis kell on?", "esn", false, true)
		if r.ReasoningEffort != "" || r.Model != k[2] {
			t.Errorf("%s %s: ReasoningEffort %q, Model %q", k[0], k[1], r.ReasoningEffort, r.Model)
		}
		if !strings.HasSuffix(r.Messages[0].Content, prompt.LangLine(lang.Estonian)) {
			t.Errorf("%s: language line missing", k[1])
		}
	}
}

func TestForkPromptReplyCap(t *testing.T) {
	lang.UseForTest(t, lang.English)
	for _, k := range []struct {
		endpoint, model string
		want            int
	}{
		{ollamaEndpoint, "qwen3.5:4b", 300},
		{"https://api.anthropic.com/v1", "claude-haiku-5-5", 400},
	} {
		useKnowledge(t, "custom", k.endpoint, k.model)
		if r := CreateAIReq("hi", "esn", false, true); r.MaxCompletionTokens != k.want {
			t.Errorf("%s: MaxCompletionTokens = %d, want %d", k.model, r.MaxCompletionTokens, k.want)
		}
		cam := openai.ChatCompletionRequest{Model: k.model, MaxCompletionTokens: 2048}
		forkCameraReq(&cam)()
		if cam.MaxCompletionTokens != k.want {
			t.Errorf("%s camera: MaxCompletionTokens = %d, want %d", k.model, cam.MaxCompletionTokens, k.want)
		}
	}
}

// A saved profile whose brain differs from Knowledge sets the cap, on the request and the camera follow-up.
func TestForkPromptReplyCapProfile(t *testing.T) {
	for _, k := range []struct {
		endpoint, model string
		profile         llm.Profile
		want            int
	}{
		{ollamaEndpoint, "qwen3.5:4b", llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}, 400},
		{"https://api.anthropic.com/v1", "claude-haiku-5-5", llm.Profile{Provider: llm.Ollama, Model: "qwen3.5:4b"}, 300},
	} {
		useKnowledge(t, "custom", k.endpoint, k.model)
		saveLLM(t, lang.English, llm.Config{Profiles: map[string]llm.Profile{lang.English: k.profile}, ClaudeKey: strings.Repeat("k", 20)})
		if r := CreateAIReq("hi", "esn", false, true); r.MaxCompletionTokens != k.want || r.Model != k.profile.Model {
			t.Errorf("%s profile: MaxCompletionTokens %d model %q, want %d", k.profile.Provider, r.MaxCompletionTokens, r.Model, k.want)
		}
		cam := openai.ChatCompletionRequest{Model: k.model}
		forkCameraReq(&cam)()
		if cam.MaxCompletionTokens != k.want {
			t.Errorf("%s profile camera: MaxCompletionTokens = %d, want %d", k.profile.Provider, cam.MaxCompletionTokens, k.want)
		}
	}
}

func TestForkActionsNoParam(t *testing.T) {
	a := GetActionsFromString("Look. {{getImage}}")
	if len(a) < 2 || a[1].Action != ActionGetImage || a[1].Parameter != "" {
		t.Errorf("{{getImage}} parsed as %+v", a)
	}
}

// Every {{command||param}} the block offers must parse: the command exists and lists the param.
func TestForkCommandsSample(t *testing.T) {
	useKnowledge(t, "custom", ollamaEndpoint, "qwen3.5:4b")
	vars.APIConfig.Knowledge.CommandsEnable, vars.APIConfig.Knowledge.SaveChat = true, true
	byName := map[string]LLMCommand{}
	for _, c := range ValidLLMCommands {
		byName[c.Command] = c
	}
	if byName["playAnimationWI"].ParamChoices != prompt.SampleEmotions {
		t.Errorf("prompt.SampleEmotions drifted from playAnimationWI ParamChoices %q", byName["playAnimationWI"].ParamChoices)
	}
	block := forkCommands(true)
	offered := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{\{(\w+)\|\|(\w+)\}\}`).FindAllStringSubmatch(block, -1) {
		name, param := m[1], m[2]
		if name == "command" {
			continue // the grammar line itself
		}
		if param == "emotion" {
			param = "love" // placeholder: the list is SampleEmotions
		}
		offered[name] = true
		if !slices.Contains(strings.Split(byName[name].ParamChoices, ", "), param) {
			t.Errorf("block offers {{%s||%s}}, not in its ParamChoices", name, param)
		}
		a := GetActionsFromString("{{" + name + "||" + param + "}}")
		if len(a) == 0 || a[0].Action != byName[name].Action || a[0].Parameter != param {
			t.Errorf("block offers {{%s||%s}}; the parser returns %+v", m[1], m[2], a)
		}
	}
	for _, name := range []string{"playAnimationWI", "playAnimation", "getImage", "newVoiceRequest"} {
		if !offered[name] {
			t.Errorf("block lacks {{%s||…}}", name)
		}
	}
	if strings.Contains(forkCommands(false), "newVoiceRequest") {
		t.Error("intent-graph path offers newVoiceRequest")
	}
	vars.APIConfig.Knowledge.SaveChat = false
	if strings.Contains(forkCommands(true), "newVoiceRequest") {
		t.Error("newVoiceRequest offered without SaveChat")
	}
	vars.APIConfig.Knowledge.CommandsEnable = false
	if forkCommands(true) != "" {
		t.Error("commands block rendered with CommandsEnable off")
	}
}

func TestForkCameraEstonianUnloads(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model     string `json:"model"`
			KeepAlive *int   `json:"keep_alive"`
		}
		json.NewDecoder(r.Body).Decode(&b)
		if r.URL.Path != "/api/generate" || b.KeepAlive == nil || *b.KeepAlive != 0 {
			t.Errorf("unload request %s %+v", r.URL.Path, b)
		}
		mu.Lock()
		got = append(got, b.Model)
		mu.Unlock()
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	old := llm.OllamaPort
	llm.OllamaPort = u.Port()
	t.Cleanup(func() { llm.OllamaPort = old })
	useKnowledge(t, "custom", srv.URL+"/v1", "qwen3.5:4b")

	for _, c := range []struct {
		lang   string
		before []string
		after  []string
	}{
		{lang.English, nil, nil},
		{lang.Estonian, []string{llm.DefaultETModel}, []string{llm.DefaultETModel, "qwen3.5:4b"}},
	} {
		lang.UseForTest(t, c.lang)
		got = nil
		r := CreateAIReq("mida sa näed?", "esn", false, true)
		r.Model = vars.APIConfig.Knowledge.Model // DoGetImage's own model pick
		r.ReasoningEffort = ""
		done := forkCameraReq(&r)
		if r.ReasoningEffort != "none" || r.Model != "qwen3.5:4b" {
			t.Errorf("%s: camera request ReasoningEffort %q, Model %q", c.lang, r.ReasoningEffort, r.Model)
		}
		if strings.Join(got, ",") != strings.Join(c.before, ",") {
			t.Errorf("%s: unloads before photo = %v, want %v", c.lang, got, c.before)
		}
		done()
		if strings.Join(got, ",") != strings.Join(c.after, ",") {
			t.Errorf("%s: unloads after photo = %v, want %v", c.lang, got, c.after)
		}
	}
}
