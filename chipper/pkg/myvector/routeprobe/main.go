// Command routeprobe shows what wire-pod does with a sentence — intent + params, plugin reply, or LLM answer — without the robot.
//
//	go run ./pkg/myvector/routeprobe [-lang et-EE] [-audio] [-llm] [-plugins] "text"   # run from chipper/, beside apiConfig.json
//
// -lang / -brain test another language / LLM profile from a temp copy of myvectorConfig.json (the saved file is never written).
// -audio speaks the text in the language's voice (TTS :8091) and routes the STT (:8092) transcript; STT serves only the SAVED lang.
// Output lines: "stt: …", then "intent: <name> <params>" | "plugin: <reply>" | "llm: <answer>" | "llm: (not asked)".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
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

func main() {
	l := flag.String("lang", "", "en-US or et-EE (default: the saved language)")
	audio := flag.Bool("audio", false, "route the TTS→STT round trip of the text")
	ask := flag.Bool("llm", false, "ask the LLM when no intent matches")
	brain := flag.String("brain", "", "provider:model for this language's LLM profile, e.g. ollama:qwen3.5:4b")
	plugins := flag.Bool("plugins", false, "load ./plugins/*.so first (a plugin match RUNS the plugin)")
	tts := flag.String("tts", "http://127.0.0.1:8091", "TTS service")
	stt := flag.String("stt", "http://127.0.0.1:8092", "STT service")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	logger.Init() // wire-pod's log lines only with DEBUG_LOGGING=true
	vars.Init()
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
	a, err := ttr.ForkProbeLLM(ctx, text, esn)
	if err != nil {
		die(1, "llm: %v", err)
	}
	fmt.Println("llm: " + strings.Join(strings.Fields(a), " "))
}

// override points lang.Path at a temp copy of the settings file with "lang" = l and, given brain, llm[l] = brain.
func override(l, brain string) {
	m := map[string]any{}
	if b, err := os.ReadFile(lang.Path()); err == nil {
		json.Unmarshal(b, &m)
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
	b, _ := json.Marshal(m)
	dir, err := os.MkdirTemp("", "routeprobe")
	if err != nil {
		die(2, "%v", err)
	}
	p := filepath.Join(dir, "myvectorConfig.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		die(2, "%v", err)
	}
	lang.Path = func() string { return p }
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
	os.Exit(code)
}
