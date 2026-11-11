package wirepod_ttr

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/sashabaranov/go-openai"
)

// forkRelayFake is a brain estllm:x and a vision llava:7b on one fake Ollama.
func forkRelayFake(t *testing.T) (*fakeLLM, llm.Target, llm.Target) {
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", "") // a developer's own trace dir stays untouched
	f := newFakeLLM(t)
	f.replies, f.cut, f.down = map[string][]string{}, map[string]bool{}, map[string]int{}
	return f, llm.Target{Provider: llm.Ollama, BaseURL: f.URL + "/v1", Model: "estllm:x"}, llm.Target{Provider: llm.Ollama, BaseURL: f.URL + "/v1", Model: "llava:7b"}
}

// forkPhotoMsgs is DoGetImage's photo conversation; q is the user's question, "" for none.
func forkPhotoMsgs(q string) []openai.ChatCompletionMessage {
	msgs := []openai.ChatCompletionMessage{{Role: "system", Content: "persona"}}
	if q != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{Role: "user", Content: q})
	}
	return append(msgs, openai.ChatCompletionMessage{Role: "assistant", Content: "{{getImage||front}}"},
		openai.ChatCompletionMessage{Role: "user", MultiContent: []openai.ChatMessagePart{{Type: openai.ChatMessagePartTypeImageURL,
			ImageURL: &openai.ChatMessageImageURL{URL: "data:image/jpeg;base64,AA==", Detail: openai.ImageURLDetailLow}}}})
}

// forkPhotoStream reads r as DoGetImage does: the joined deltas and the last finish_reason.
func forkPhotoStream(c *openai.Client, r openai.ChatCompletionRequest) (string, openai.FinishReason, error) {
	r.Stream, r.StreamOptions = true, &openai.StreamOptions{IncludeUsage: true}
	s, err := c.CreateChatCompletionStream(context.Background(), r)
	if err != nil {
		return "", "", err
	}
	defer s.Close()
	var text string
	var finish openai.FinishReason
	for {
		resp, err := s.Recv()
		if errors.Is(err, io.EOF) {
			return text, finish, nil
		}
		if err != nil {
			return text, finish, err
		}
		text += resp.Choices[0].Delta.Content
		finish = resp.Choices[0].FinishReason
	}
}

// forkRelayStream streams the photo request (q: the user's question) through a relay client.
func forkRelayStream(brain, vision llm.Target, q string) (string, openai.FinishReason, error) {
	return forkPhotoStream(forkRelayClient(brain, vision), openai.ChatCompletionRequest{Model: vision.Model, Messages: forkPhotoMsgs(q)})
}

func TestForkRelay(t *testing.T) {
	f, brain, vision := forkRelayFake(t)
	f.replies["estllm:x"] = []string{"What colour is the cup?", "Valge tass."}
	f.replies["llava:7b"] = []string{"{{playAnimationWI||happy}} A white cup."}
	f.cut["llava:7b"] = true
	text, finish, err := forkRelayStream(brain, vision, "mis värvi on tass?")
	if err != nil {
		t.Fatal(err)
	}
	if want := "chat estllm:x,unload estllm:x,chat llava:7b,unload llava:7b,chat estllm:x"; strings.Join(f.events, ",") != want {
		t.Errorf("events %v, want %s", f.events, want)
	}
	if text != "{{playAnimationWI||happy}} Valge tass." || finish != openai.FinishReasonLength {
		t.Errorf("stream %q %q", text, finish)
	}
	hop0, hop1, hop2 := f.bodies[0], f.bodies[1], f.bodies[2]
	if !strings.Contains(hop0, "mis värvi on tass?") || !strings.Contains(hop2, `"Tõlgi eesti keelde:\nA white cup."`) || strings.Contains(hop2, "{{") {
		t.Errorf("hop 0 %s\nhop 2 %s", hop0, hop2)
	}
	if !strings.Contains(hop1, `"stream":false`) || strings.Contains(hop1, "stream_options") ||
		!strings.Contains(hop1, `[{"text":"What colour is the cup?","type":"text"},{"image_url"`) {
		t.Errorf("hop 1 %s", hop1)
	}
}

// Hop 0 down or no question: the vision model gets forkRelayAsk, the relay still answers in Estonian.
func TestForkRelayHop0Down(t *testing.T) {
	for _, q := range []string{"mis värvi on tass?", ""} {
		f, brain, vision := forkRelayFake(t)
		f.replies["estllm:x"] = []string{"", "Valge tass."}
		if q == "" {
			f.replies["estllm:x"] = f.replies["estllm:x"][1:]
		}
		f.replies["llava:7b"] = []string{"A white cup."}
		text, finish, err := forkRelayStream(brain, vision, q)
		if err != nil || text != "Valge tass." || finish != openai.FinishReasonStop {
			t.Errorf("q %q: stream %q %q %v", q, text, finish, err)
		}
		if hop1 := f.bodies[len(f.bodies)-2]; !strings.Contains(hop1, `"text":"`+forkRelayAsk+`"`) || !strings.Contains(hop1, `"content":"`+q+`"`) && q != "" {
			t.Errorf("q %q: hop 1 %s", q, hop1)
		}
		want := "unload estllm:x,chat llava:7b,unload llava:7b,chat estllm:x"
		if q != "" {
			want = "chat estllm:x," + want
		}
		if strings.Join(f.events, ",") != want {
			t.Errorf("q %q: events %v, want %s", q, f.events, want)
		}
	}
}

// Hop 1 or hop 2 down, or hop 2 empty: the stream errors (DoGetImage → forkVisionDown) and the vision model is unloaded.
func TestForkRelayHop2Down(t *testing.T) {
	for _, c := range []struct {
		name   string
		set    func(*fakeLLM)
		events string
	}{
		{"hop 2 empty", func(f *fakeLLM) { f.replies["estllm:x"] = []string{"Q?", ""} }, "chat estllm:x,unload estllm:x,chat llava:7b,unload llava:7b,chat estllm:x"},
		{"hop 1 500", func(f *fakeLLM) { f.down["llava:7b"] = http.StatusInternalServerError }, "chat estllm:x,unload estllm:x,chat llava:7b,unload llava:7b"},
	} {
		f, brain, vision := forkRelayFake(t)
		c.set(f)
		if _, _, err := forkRelayStream(brain, vision, "mis on?"); err == nil {
			t.Errorf("%s: no stream error", c.name)
		}
		if strings.Join(f.events, ",") != c.events {
			t.Errorf("%s: events %v, want %s", c.name, f.events, c.events)
		}
	}
}

// Commands-only hop 1: no hop 2, the commands go through as they are.
func TestForkRelayCommandsOnly(t *testing.T) {
	f, brain, vision := forkRelayFake(t)
	f.replies["llava:7b"] = []string{"{{playAnimationWI||happy}}"}
	text, _, err := forkRelayStream(brain, vision, "")
	if err != nil || text != "{{playAnimationWI||happy}}" || strings.Join(f.events, ",") != "unload estllm:x,chat llava:7b,unload llava:7b" {
		t.Errorf("stream %q %v events %v", text, err, f.events)
	}
}

// R1/R4/R7 wired: hop 0/2 on the brain's fake, hop 1 (English Ollama tier + hop 0's question) on the vision fake, the
// photo never at the brain, unloads only inside the relay (none before the stream).
func TestForkCameraRelay(t *testing.T) {
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	for _, c := range []struct {
		name               string
		brain              llm.Profile
		listening, legacy  bool // legacy: no saved profiles, Knowledge "custom" Ollama (et brain = DefaultETModel)
		brainModel, unload string
	}{
		{"ollama brain", llm.Profile{Provider: llm.Ollama, Model: "estllm:x"}, false, false, "estllm:x", "estllm:x," + llm.DefaultVisionET},
		{"claude brain", claude, false, false, "claude-haiku-5-5", llm.DefaultVisionET},
		{"listening", claude, true, false, listen.Model, listen.Model + "," + llm.DefaultVisionET},
		{"legacy ollama", llm.Profile{}, false, true, llm.DefaultETModel, llm.DefaultETModel + "," + llm.DefaultVisionET},
	} {
		local, cl := newFakeLLM(t), newFakeLLM(t)
		for _, f := range []*fakeLLM{local, cl} {
			f.replies = map[string][]string{c.brainModel: {"What colour is the cup?", "Valge tass."}, llm.DefaultVisionET: {"A white cup."}}
		}
		useAnthropic(t, cl.URL)
		useKnowledge(t, "openai", "", "gpt-4.1")
		if c.legacy {
			old := llm.OllamaPort
			llm.OllamaPort = local.URL[strings.LastIndex(local.URL, ":")+1:]
			t.Cleanup(func() { llm.OllamaPort = old })
			useKnowledge(t, "custom", local.URL+"/v1", "llama:k")
			lang.UseForTest(t, lang.Estonian)
		} else {
			saveLLM(t, lang.Estonian, llm.Config{OllamaURL: local.URL, ClaudeKey: testClaudeKey, Profiles: map[string]llm.Profile{lang.Estonian: c.brain}})
		}
		freshListening(t)
		if c.listening {
			forkListening.Activate("00e20100", forkNow())
		}
		brainFake := local
		if c.brainModel == claude.Model {
			brainFake = cl
		}
		r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: forkPhotoMsgs("mis värvi on tass?")}
		client := openai.NewClient("")
		done := forkCameraReq(&r, &client, "esn")
		if len(local.unloads) != 0 {
			t.Errorf("%s: unloads before the stream %v", c.name, local.unloads)
		}
		text, _, err := forkPhotoStream(client, r)
		done()
		if err != nil || text != "Valge tass." || strings.Join(local.unloads, ",") != c.unload {
			t.Errorf("%s: stream %q %v unloads %v, want %s", c.name, text, err, local.unloads, c.unload)
		}
		var hop1 string
		for _, b := range local.bodies {
			if strings.Contains(b, "image_url") {
				hop1 = b
			}
		}
		if !strings.Contains(hop1, `"model":"`+llm.DefaultVisionET+`"`) || !strings.Contains(hop1, `"text":"What colour is the cup?"`) ||
			!strings.Contains(hop1, prompt.SpeechBlock(prompt.Ollama)) || strings.Contains(hop1, prompt.LangLine(prompt.OllamaEt, lang.Estonian)) ||
			!strings.Contains(hop1, prompt.PhotoLine(prompt.Ollama)) || strings.Contains(hop1, "takes a photo") || strings.Contains(hop1, "mis värvi on tass?") {
			t.Errorf("%s: hop 1 %s", c.name, hop1)
		}
		chats := 0
		for _, body := range brainFake.bodies {
			if strings.Contains(body, `"model":"`+c.brainModel+`"`) {
				chats++
				if strings.Contains(body, "image_url") {
					t.Errorf("%s: the photo reached the brain: %s", c.name, body)
				}
			}
		}
		if chats != 2 || (brainFake == local && cl.path != "") {
			t.Errorf("%s: brain chats %d, claude fake %q", c.name, chats, cl.path)
		}
	}
}

// R1's other branches: one direct chat on the old path, no relay.
func TestForkCameraRelaySkips(t *testing.T) {
	ollama, llava := llm.Profile{Provider: llm.Ollama, Model: "estllm:x"}, llm.Profile{Provider: llm.Ollama, Model: "llava:7b"}
	claude := llm.Profile{Provider: llm.Claude, Model: "claude-haiku-5-5"}
	for _, c := range []struct {
		name, l, want string
		cfg           *llm.Config
	}{
		{"en-US", lang.English, "llava:7b", &llm.Config{Profiles: map[string]llm.Profile{lang.English: ollama}, Vision: map[string]llm.Profile{lang.English: llava}}},
		{"legacy, no vision target", lang.Estonian, "gpt-4.1", nil},
		{"claude vision", lang.Estonian, "claude-haiku-5-5", &llm.Config{Profiles: map[string]llm.Profile{lang.Estonian: ollama}, Vision: map[string]llm.Profile{lang.Estonian: claude}}},
		{"same model", lang.Estonian, llm.DefaultVisionET, &llm.Config{Profiles: map[string]llm.Profile{lang.Estonian: {Provider: llm.Ollama, Model: llm.DefaultVisionET}}}},
	} {
		f := newFakeLLM(t)
		useAnthropic(t, f.URL)
		useKnowledge(t, "openai", "", "gpt-4.1")
		freshListening(t)
		lang.UseForTest(t, c.l)
		if c.cfg != nil {
			c.cfg.OllamaURL, c.cfg.ClaudeKey = f.URL, testClaudeKey
			saveLLM(t, c.l, *c.cfg)
		}
		cfg := openai.DefaultConfig("")
		cfg.BaseURL = f.URL + "/v1"
		client := openai.NewClientWithConfig(cfg)
		r := openai.ChatCompletionRequest{Model: openai.GPT4oMini, Messages: forkPhotoMsgs("mis värvi on tass?")}
		forkCameraReq(&r, &client, "esn")()
		if _, err := client.CreateChatCompletion(context.Background(), r); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var chats []string
		for _, e := range f.events {
			if strings.HasPrefix(e, "chat ") {
				chats = append(chats, e)
			}
		}
		if strings.Join(chats, ",") != "chat "+c.want {
			t.Errorf("%s: chats %v, want one chat %s", c.name, chats, c.want)
		}
		tier := map[string]string{"en-US": prompt.Ollama, "same model": prompt.OllamaEt}[c.name] // Ollama vision: a photo prompt
		if body := f.bodies[len(f.bodies)-1]; tier != "" && !strings.Contains(body, prompt.PhotoLine(tier)) {
			t.Errorf("%s: vision body lacks PhotoLine: %s", c.name, body)
		}
	}
}

// A relay turn with the trace dir present saves every hop's request + reply in trace.md and the photo as sent.
func TestForkRelayTrace(t *testing.T) {
	f, brain, vision := forkRelayFake(t)
	root := t.TempDir()
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", root)
	f.replies["estllm:x"] = []string{"What colour is the cup?", "Valge tass."}
	f.replies["llava:7b"] = []string{"{{playAnimationWI||happy}} A white cup."}
	if _, _, err := forkRelayStream(brain, vision, "mis värvi on tass?"); err != nil {
		t.Fatal(err)
	}
	dirs, _ := os.ReadDir(root)
	if len(dirs) != 1 {
		t.Fatalf("trace dirs %v", dirs)
	}
	d := filepath.Join(root, dirs[0].Name())
	md, err := os.ReadFile(filepath.Join(d, "trace.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	order := []string{"mis värvi on tass?", "## hop 0 to English request", "## hop 0 to English reply — estllm:x",
		"## hop 1 vision request", "### [0] system", "persona", "text part:\n````\nWhat colour is the cup?", "image part: [photo.jpg]",
		"## hop 1 vision reply — llava:7b", "A white cup.", "## hop 2 to Estonian request", "## final reply", "{{playAnimationWI||happy}} Valge tass."}
	at := 0
	for _, w := range order {
		i := strings.Index(s[at:], w)
		if i < 0 {
			t.Fatalf("trace.md lacks %q after byte %d:\n%s", w, at, s)
		}
		at += i + len(w)
	}
	hop1 := s[strings.Index(s, "## hop 1 vision request"):strings.Index(s, "## hop 1 vision reply")]
	if !strings.Contains(hop1, "- stream: `false`") || strings.Contains(hop1, "stream_options") || !strings.Contains(s, f.URL+"/v1/chat/completions") {
		t.Errorf("hop 1 trace is not the wire body:\n%s", hop1)
	}
	if strings.Contains(s, "base64") {
		t.Errorf("trace.md holds the image data:\n%s", s)
	}
	if b, err := os.ReadFile(filepath.Join(d, "photo.jpg")); err != nil || string(b) != "\x00" {
		t.Errorf("photo.jpg %q %v", b, err)
	}
}

// No trace dir: nothing is created, all three hops run on a nil trace.
func TestForkRelayTraceOff(t *testing.T) {
	f, brain, vision := forkRelayFake(t)
	absent := filepath.Join(t.TempDir(), "absent")
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", absent)
	f.replies["estllm:x"] = []string{"What colour is the cup?", "Valge tass."}
	f.replies["llava:7b"] = []string{"A white cup."}
	if text, _, err := forkRelayStream(brain, vision, "mis värvi on tass?"); err != nil || text != "Valge tass." {
		t.Errorf("stream %q %v", text, err)
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Errorf("trace dir created: %v", err)
	}
}

// A failed hop 2 ends the trace with its failure.
func TestForkRelayTraceHop2Failed(t *testing.T) {
	f, brain, vision := forkRelayFake(t)
	root := t.TempDir()
	t.Setenv("MYVECTOR_VISION_TRACE_DIR", root)
	f.replies["estllm:x"] = []string{"Q?", ""}
	f.replies["llava:7b"] = []string{"A cup."}
	if _, _, err := forkRelayStream(brain, vision, "mis on?"); err == nil {
		t.Fatal("no stream error")
	}
	dirs, _ := os.ReadDir(root)
	if len(dirs) != 1 {
		t.Fatalf("trace dirs %v", dirs)
	}
	md, _ := os.ReadFile(filepath.Join(root, dirs[0].Name(), "trace.md"))
	if !strings.HasSuffix(string(md), "## hop 2 failed\n\n````\nempty reply\n````\n\n") {
		t.Errorf("trace.md tail:\n%s", md)
	}
}

// A bare photo phrase skips hop 0: the vision model gets forkRelayAsk, the brain only translates the answer.
func TestForkRelayBarePhrase(t *testing.T) {
	for _, q := range []string{"Mida sa näed?", "hei vektor, vaata", "tee pilt"} {
		f, brain, vision := forkRelayFake(t)
		f.replies["estllm:x"] = []string{"Valge tass."}
		f.replies["llava:7b"] = []string{"A white cup."}
		text, _, err := forkRelayStream(brain, vision, q)
		if err != nil || text != "Valge tass." {
			t.Errorf("q %q: stream %q %v", q, text, err)
		}
		if want := "unload estllm:x,chat llava:7b,unload llava:7b,chat estllm:x"; strings.Join(f.events, ",") != want {
			t.Errorf("q %q: events %v, want %s", q, f.events, want)
		}
		if hop1 := f.bodies[0]; !strings.Contains(hop1, `"text":"`+forkRelayAsk+`"`) {
			t.Errorf("q %q: hop 1 %s", q, hop1)
		}
	}
}

func TestForkPhotoBare(t *testing.T) {
	for text, want := range map[string]bool{
		"Mida sa näed?": true, "mis sa näed": true, "Hei Vektor, tee foto!": true, "vektor vaata": true, "tee pilti": true, "Tee fotot.": true,
		"mida sa näed laual?": false, "vaata seda tassi": false, "tee pilt ja ütle värv": false, "": false,
	} {
		if got := forkPhotoBare(lang.Estonian, text); got != want {
			t.Errorf("forkPhotoBare(%q) = %v, want %v", text, got, want)
		}
	}
}
