// Command chainprobe replays a follow-up chain through the real chat request path (CreateAIReq → forkAIReq splices
// the per-ESN remembered chat) without the robot, per arm × rep, one JSON line per call.
//
//	go run ./pkg/myvector/chainprobe -chain c.json [-chained] [-arms 0,S,Q,P,E] [-reps 5] [-brain ollama:model] [-temp 0.4 -topp 0.9] -out o.jsonl   # from chipper/
//
// c.json: {"lang":"et-EE","history":[{"user":…,"assistant":…}],"turns":["ja.","mida?"]}. Each rep starts from the
// history alone; -chained remembers each turn's own reply before the next turn. Arms: 0 = as saved; S = -temp, -topp
// (default 0.4, 0.9: routeprobe's S, now the apertus family's own, so for apertus only a non-default S differs from
// arm 0); Q = the saved ollama template without "Now and then ask a question back.";
// P = the saved ollama template with lineP (no speaking as the person, no invented past) right after that line;
// E = the et EstLLM brain. Like routeprobe, nothing under secrets/server/ is written (temp copies), web search is off.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	ttr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/ttr"
	"github.com/sashabaranov/go-openai"
)

const (
	lineQ  = "Now and then ask a question back. "
	lineP  = "Never speak as the person or put words in their mouth, and never invent past events or conversations. "
	brainE = "ollama:hf.co/mradermacher/Llama-3.1-EstLLM-8B-Instruct-1125-i1-GGUF:i1-Q4_K_M"
)

type chain struct {
	Lang    string `json:"lang"`
	History []struct {
		User      string `json:"user"`
		Assistant string `json:"assistant"`
	} `json:"history"`
	Turns []string `json:"turns"`
}

type line struct {
	Arm    string  `json:"arm"`
	Temp   float32 `json:"temp,omitempty"`
	TopP   float32 `json:"top_p,omitempty"`
	Rep    int     `json:"rep"`
	Turn   int     `json:"turn"`
	Ask    string  `json:"ask"`
	Reply  string  `json:"reply"`
	Raw    string  `json:"raw"`
	Finish string  `json:"finish"`
	Target string  `json:"target"`
	Prompt int     `json:"prompt_tokens"`
	Hist   int     `json:"history_msgs"`
	Ms     int64   `json:"ms"`
	Err    string  `json:"err,omitempty"`
}

var tmp string

func main() {
	chainFile := flag.String("chain", "", "chain JSON file")
	chained := flag.Bool("chained", false, "remember each turn's own reply before the next turn")
	armList := flag.String("arms", "0", "comma list of 0, S, Q, P, E")
	reps := flag.Int("reps", 1, "reps per arm")
	brain := flag.String("brain", "", "provider:model for the chain language's profile (not arm E)")
	out := flag.String("out", "", "JSONL output file (appended)")
	temp := flag.Float64("temp", 0.4, "arm S temperature, > 0 (0 would fall back to the Modelfile value)")
	topP := flag.Float64("topp", 0.9, "arm S top_p, in (0, 1]")
	flag.Parse()
	if *chainFile == "" || *out == "" || *reps < 1 || !(*temp > 0 && *temp < 10) || !(*topP > 0 && *topP <= 1) {
		flag.Usage()
		os.Exit(2)
	}
	var c chain
	b, err := os.ReadFile(*chainFile)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	if err != nil || !lang.Valid(c.Lang) || len(c.Turns) == 0 {
		die("chain: %v (lang %q, %d turns)", err, c.Lang, len(c.Turns))
	}
	arms := strings.Split(*armList, ",")
	for _, a := range arms {
		if a != "0" && a != "S" && a != "Q" && a != "P" && a != "E" {
			die("arms: 0, S, Q, P, E")
		}
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		die("%v", err)
	}
	defer f.Close()
	logger.Init()
	defer func() { os.RemoveAll(tmp) }() // reads tmp at exit: isolate() sets it below
	isolate()
	sc := search.LoadConfig()
	sc.Provider = "off"
	if err := search.Save(sc); err != nil {
		die("search off: %v", err)
	}
	vars.Init()
	vars.CustomIntents, vars.CustomIntentsExist = nil, false
	if vars.APIConfig.Knowledge.SaveChat {
		die("SaveChat is on: forkAIReq would not splice the history the way live does")
	}
	if len(vars.BotInfo.Robots) == 0 {
		die("no robot in botSdkInfo")
	}
	esn := vars.BotInfo.Robots[0].Esn
	basePath, ePath := override(c.Lang, *brain), override(c.Lang, brainE)
	lang.Path = func() string { return basePath }
	raw, err := os.ReadFile(prompt.Path())
	if err != nil {
		die("prompt: %v", err)
	}
	baseTmpl := prompt.Load().Live(prompt.Ollama)
	if n := strings.Count(baseTmpl, lineQ); n != 1 {
		die("arm Q: %q appears %d times in the live ollama template, want 1", lineQ, n)
	}
	enc := json.NewEncoder(f)
	for _, arm := range arms {
		// restore the base store and sampling, then apply the arm
		if err := os.WriteFile(prompt.Path(), raw, 0o600); err != nil {
			die("%v", err)
		}
		llm.ProbeSampling = nil
		lang.Path = func() string { return basePath }
		switch arm {
		case "S":
			s := llm.Sampling{Family: "probe-S", Temperature: float32(*temp), TopP: float32(*topP)}
			llm.ProbeSampling = func(provider, _ string) (llm.Sampling, bool) { return s, provider != llm.Claude }
		case "Q":
			t := strings.Replace(baseTmpl, lineQ, "", 1)
			if err := prompt.Save(prompt.Ollama, &t, nil); err != nil {
				die("arm Q: %v", err)
			}
			if strings.Contains(prompt.Load().Live(prompt.Ollama), lineQ) {
				die("arm Q: the saved template still has the line")
			}
		case "P":
			t := strings.Replace(baseTmpl, lineQ, lineQ+lineP, 1)
			if err := prompt.Save(prompt.Ollama, &t, nil); err != nil {
				die("arm P: %v", err)
			}
			if !strings.Contains(prompt.Load().Live(prompt.Ollama), lineP) {
				die("arm P: the saved template lacks the line")
			}
		case "E":
			lang.Path = func() string { return ePath }
		}
		for rep := 1; rep <= *reps; rep++ {
			forget(esn)
			for _, h := range c.History {
				remember(h.User, h.Assistant, esn)
			}
			for i, ask := range c.Turns {
				l := line{Arm: arm, Rep: rep, Turn: i + 1, Ask: ask, Hist: len(ttr.GetChat(esn).Chats)}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				res, err := ttr.ForkProbeLLMFull(ctx, ask, esn)
				cancel()
				l.Reply, l.Raw, l.Finish, l.Target = strings.Join(strings.Fields(res.Answer), " "), res.RawAnswer, res.Finish, res.Target
				l.Prompt, l.Ms = res.Usage.PromptTokens, res.Dur.Milliseconds()
				if arm == "S" {
					l.Temp, l.TopP = float32(*temp), float32(*topP)
				}
				if err != nil {
					l.Err = err.Error()
				}
				if err := enc.Encode(l); err != nil {
					die("%v", err)
				}
				fmt.Printf("%s %d %d [%s %dtok %dms] %s → %s\n", arm, rep, i+1, l.Target, l.Prompt, l.Ms, ask, l.Reply)
				if *chained && err == nil && l.Reply != "" {
					remember(ask, l.Reply, esn) // what forkAfterReply remembers: the spoken sentences joined, G2 repeats dropped (forkProbeSpoken); an empty reply remembers no turn, as live
				}
			}
		}
	}
	llm.ProbeSampling = nil
}

func remember(user, assistant, esn string) {
	ttr.Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: user},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: assistant}, esn)
}

// forget drops esn's remembered chat (a fresh history per rep).
func forget(esn string) {
	kept := vars.RememberedChats[:0]
	for _, ch := range vars.RememberedChats {
		if ch.ESN != esn {
			kept = append(kept, ch)
		}
	}
	vars.RememberedChats = kept
}

// isolate is routeprobe's: apiConfig, myvectorConfig, prompt, memory and jdocs from temp copies; no robot speech.
func isolate() {
	ttr.ForkProbeMute(func(_, text string) error { return nil })
	src := vars.ApiConfigPath
	b, err := os.ReadFile(src)
	if err != nil {
		die("%v (run from chipper/)", err)
	}
	var c struct {
		STT struct {
			Service string `json:"provider"`
		} `json:"STT"`
	}
	if err := json.Unmarshal(b, &c); err != nil {
		die("%s: %v", src, err)
	}
	if os.Getenv("STT_SERVICE") == "" {
		os.Setenv("STT_SERVICE", c.STT.Service)
	}
	if tmp, err = os.MkdirTemp("", "chainprobe"); err != nil {
		die("%v", err)
	}
	os.Setenv("MYVECTOR_QUIET_FILE", filepath.Join(tmp, "quiet-until"))
	os.Setenv("MYVECTOR_NAP", "off")
	promptSrc, memSrc, langSrc := prompt.Path(), memory.Path(), lang.Path()
	vars.ApiConfigPath = filepath.Join(tmp, "apiConfig.json")
	copyFile(src, vars.ApiConfigPath, true)
	copyFile(langSrc, lang.Path(), false)
	copyFile(promptSrc, prompt.Path(), false)
	copyFile(memSrc, memory.Path(), false)
	j := filepath.Join(tmp, "jdocs.json")
	copyFile(vars.JdocsPath, j, false)
	vars.JdocsPath = j
}

func copyFile(src, dst string, must bool) {
	b, err := os.ReadFile(src)
	if errors.Is(err, os.ErrNotExist) && !must {
		return
	}
	if err != nil {
		die("%v", err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		die("%v", err)
	}
}

// override is routeprobe's, returning the new settings path instead of setting lang.Path: the settings copy with
// "lang" = l and, given brain, llm[l] = brain.
func override(l, brain string) string {
	m := map[string]any{}
	b, err := os.ReadFile(lang.Path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		die("%v", err)
	}
	if err == nil {
		if err := json.Unmarshal(b, &m); err != nil {
			die("%s: %v", lang.Path(), err)
		}
	}
	m["lang"] = l
	if brain != "" {
		p, model, ok := strings.Cut(brain, ":")
		if !ok {
			die("brain: provider:model")
		}
		profiles, _ := m["llm"].(map[string]any)
		if profiles == nil {
			profiles = map[string]any{}
		}
		profiles[l] = map[string]string{"provider": p, "model": model}
		m["llm"] = profiles
	}
	b, _ = json.Marshal(m)
	dir, err := os.MkdirTemp(tmp, "override")
	if err != nil {
		die("%v", err)
	}
	p := filepath.Join(dir, "myvectorConfig.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		die("%v", err)
	}
	return p
}

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	if tmp != "" {
		os.RemoveAll(tmp)
	}
	os.Exit(1)
}
