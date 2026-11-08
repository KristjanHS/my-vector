// Command routeprobe shows what wire-pod does with a sentence — intent + params, plugin reply, or LLM answer — without the robot.
//
//	go run ./pkg/myvector/routeprobe [-lang et-EE] [-audio] [-llm] [-plugins] "text"   # run from chipper/, beside apiConfig.json
//
// Nothing under secrets/server/ is written: apiConfig.json, myvectorConfig.json, myvectorPrompt.json and jdocs/jdocs.json are read from temp copies
// (vars.ReadConfig rewrites apiConfig.json on every start), removed on exit.
// -lang / -brain test another language / LLM profile (they edit the temp copy of myvectorConfig.json).
// -plugins / -plugin f.so / -custom RUN code on a match: plugin Action funcs, and customIntents.json's exec commands and
// Lua scripts (against the real robot). Without -custom, custom intents are not loaded.
// -plugin f.so loads one plugin; build the HA one against a dead URL to test "HA down" (scripts/live-check.sh ha).
// -audio speaks the text in the language's voice (TTS :8091) and routes the STT (:8092) transcript; STT serves only the SAVED lang.
// Output lines: "stt: …", then "intent: <name> <params>" | "plugin: <reply>" | "llm: (not asked)" |
// "llm-target: <provider> <model>" + "llm: <answer as spoken>" | "intent: intent_system_unmatched (LLM off: …)".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
	ttr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/ttr"
)

// stream stands in for the robot's intent-graph stream and keeps what wire-pod sends it.
type stream struct {
	pb.ChipperGrpc_StreamingIntentGraphServer
	sent []*pb.IntentGraphResponse
}

func (s *stream) Send(r *pb.IntentGraphResponse) error { s.sent = append(s.sent, r); return nil }

var voices = map[string]string{"en-US": "af_bella", "et-EE": "mari"}

// tmp holds the copies of the files vars.Init and the handlers would write; cleanup removes it (die calls it too).
var tmp string

func cleanup() {
	if tmp != "" {
		os.RemoveAll(tmp)
	}
}

func main() {
	l := flag.String("lang", "", "en-US or et-EE (default: the saved language)")
	audio := flag.Bool("audio", false, "route the TTS→STT round trip of the text")
	ask := flag.Bool("llm", false, "ask the LLM when no intent matches")
	brain := flag.String("brain", "", "provider:model for this language's LLM profile, e.g. ollama:qwen3.5:4b")
	plugins := flag.Bool("plugins", false, "load ./plugins/*.so first (a plugin match RUNS the plugin)")
	pluginFile := flag.String("plugin", "", "load this one plugin .so (as ttr.LoadPlugins would); a match RUNS it")
	custom := flag.Bool("custom", false, "load customIntents.json (a match RUNS its exec command / Lua script)")
	tts := flag.String("tts", "http://127.0.0.1:8091", "TTS service")
	stt := flag.String("stt", "http://127.0.0.1:8092", "STT service")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	logger.Init() // wire-pod's log lines only with DEBUG_LOGGING=true
	defer cleanup()
	isolate()
	vars.Init()
	if !*custom {
		vars.CustomIntents, vars.CustomIntentsExist = nil, false
	}
	if *l != "" && !lang.Valid(*l) {
		die(2, "lang: one of %v", lang.Supported)
	}
	if *l == "" {
		*l = lang.Current()
	}
	if *l != lang.Current() || *brain != "" {
		override(*l, *brain)
	}
	cur := lang.Current()
	vars.IntentList, _ = vars.LoadIntents()
	if *plugins {
		ttr.LoadPlugins()
	}
	if *pluginFile != "" {
		if err := loadPlugin(*pluginFile); err != nil {
			die(2, "plugin: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	text := flag.Arg(0)
	if *audio {
		var err error
		if text, err = roundTrip(ctx, *tts, *stt, cur, text); err != nil {
			die(1, "audio: %v", err)
		}
		fmt.Println("stt: " + text)
	}
	esn := ""
	if len(vars.BotInfo.Robots) > 0 {
		esn = vars.BotInfo.Robots[0].Esn
	}
	s := &stream{}
	req := &vtt.IntentGraphRequest{Stream: s, Device: esn, LangString: cur}
	if ttr.ProcessTextAll(req, text, vars.IntentList, true) {
		for _, r := range s.sent {
			if r.ResponseType == pb.IntentGraphMode_KNOWLEDGE_GRAPH {
				fmt.Println("plugin: " + r.SpokenText)
			} else if r.IntentResult != nil {
				fmt.Println(strings.TrimSpace("intent: " + r.IntentResult.Action + " " + params(r.IntentResult.Parameters)))
			}
		}
		if len(s.sent) == 0 {
			fmt.Println("plugin: (spoke on the robot)") // a plugin reply without the intent-graph stream goes through KGSim
		}
		return
	}
	if !*ask {
		fmt.Println("llm: (not asked)")
		return
	}
	target, a, err := ttr.ForkProbeLLM(ctx, text, esn)
	if errors.Is(err, ttr.ErrForkLLMOff) {
		fmt.Printf("intent: intent_system_unmatched (%v)\n", err) // what production sends instead
		return
	}
	if target != "" {
		fmt.Println("llm-target: " + target)
	}
	if err != nil {
		die(1, "llm: %v", err)
	}
	fmt.Println("llm: " + strings.Join(strings.Fields(a), " "))
}

// isolate points vars.ApiConfigPath, lang.Path, prompt.Path, memory.Path, vars.JdocsPath and the quiet lock at temp copies (run from chipper/); no nap.
// vars.ReadConfig rewrites apiConfig.json and, when env STT_SERVICE differs from it, blanks STT.provider; the copy takes that.
func isolate() {
	src := vars.ApiConfigPath
	b, err := os.ReadFile(src)
	if err != nil {
		die(2, "%v (run from chipper/)", err)
	}
	var c struct {
		STT struct {
			Service string `json:"provider"`
		} `json:"STT"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		die(2, "%s: %v", src, err)
	}
	if os.Getenv("STT_SERVICE") == "" { // as start.sh exports it from source.sh: the file's own provider
		os.Setenv("STT_SERVICE", c.STT.Service)
	}
	if tmp, err = os.MkdirTemp("", "routeprobe"); err != nil {
		die(2, "%v", err)
	}
	os.Setenv("MYVECTOR_QUIET_FILE", filepath.Join(tmp, "quiet-until")) // a probed "shut up" never locks the real robot
	os.Setenv("MYVECTOR_NAP", "off")
	promptSrc, memSrc := prompt.Path(), memory.Path()
	langSrc := lang.Path() // beside apiConfig.json (vars/fork_lang.go); follows ApiConfigPath to the copy below
	vars.ApiConfigPath = filepath.Join(tmp, "apiConfig.json")
	copyFile(src, vars.ApiConfigPath, true)
	copyFile(langSrc, lang.Path(), false)
	copyFile(promptSrc, prompt.Path(), false)
	copyFile(memSrc, memory.Path(), false) // probed turns capture into tmp, never the real episode log
	j := filepath.Join(tmp, "jdocs.json")
	copyFile(vars.JdocsPath, j, false)
	vars.JdocsPath = j
}

// copyFile copies src to dst (0600: the copies hold keys); a missing src is fatal only when must.
func copyFile(src, dst string, must bool) {
	b, err := os.ReadFile(src)
	if errors.Is(err, os.ErrNotExist) && !must {
		return
	}
	if err != nil {
		die(2, "%v", err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		die(2, "%v", err)
	}
}

// override points lang.Path at a new temp file: the settings copy with "lang" = l and, given brain, llm[l] = brain.
func override(l, brain string) {
	m := map[string]any{}
	b, err := os.ReadFile(lang.Path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		die(2, "%v", err)
	}
	if err == nil {
		if err := json.Unmarshal(b, &m); err != nil {
			die(2, "%s: %v", lang.Path(), err)
		}
	}
	m["lang"] = l
	if brain != "" {
		p, model, ok := strings.Cut(brain, ":")
		if !ok {
			die(2, "brain: provider:model")
		}
		profiles, _ := m["llm"].(map[string]any)
		if profiles == nil {
			profiles = map[string]any{}
		}
		profiles[l] = map[string]string{"provider": p, "model": model}
		m["llm"] = profiles
	}
	b, _ = json.Marshal(m)
	dir := filepath.Join(tmp, "override") // a new Path: lang caches Current per Path
	if err := os.Mkdir(dir, 0o700); err != nil {
		die(2, "%v", err)
	}
	p := filepath.Join(dir, "myvectorConfig.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		die(2, "%v", err)
	}
	lang.Path = func() string { return p }
}

// loadPlugin registers one plugin file the way ttr.LoadPlugins registers ./plugins/*.so.
func loadPlugin(path string) error {
	p, err := plugin.Open(path)
	if err != nil {
		return err
	}
	u, err := p.Lookup("Utterances")
	if err != nil {
		return err
	}
	a, err := p.Lookup("Action")
	if err != nil {
		return err
	}
	n, err := p.Lookup("Name")
	if err != nil {
		return err
	}
	us, ok1 := u.(*[]string)
	af, ok2 := a.(func(string, string, string, string) (string, string))
	ns, ok3 := n.(*string)
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("%s: Utterances/Action/Name have the wrong types", path)
	}
	ttr.PluginUtterances = append(ttr.PluginUtterances, us)
	ttr.PluginFunctions = append(ttr.PluginFunctions, af)
	ttr.PluginNames = append(ttr.PluginNames, *ns)
	return nil
}

func roundTrip(ctx context.Context, tts, stt, l, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{"text": text, "voice": voices[l], "lang": l})
	pcm, err := post(ctx, tts+"/tts", "application/json", body)
	if err != nil {
		return "", fmt.Errorf("tts: %w", err)
	}
	out, err := post(ctx, stt+"/stt?lang="+url.QueryEscape(l), "application/octet-stream", pcm)
	if err != nil {
		return "", fmt.Errorf("stt: %w", err)
	}
	var r struct{ Text, Error string }
	if err := json.Unmarshal(out, &r); err != nil || r.Error != "" {
		return "", fmt.Errorf("stt: %s %v", r.Error, err)
	}
	return r.Text, nil
}

func post(ctx context.Context, u, ct string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ct)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(b))
	}
	return b, err
}

func params(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func die(code int, f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	cleanup()
	os.Exit(code)
}
