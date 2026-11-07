// Command followupprobe scores candidate "listen again?" prompts (follow-up-listening.md (docs/plans/) Stage 1.1).
//
//	go run ./pkg/myvector/followupprobe [-config ../../../my-vector/chipper/myvectorConfig.json]   # from chipper/
//
// Each language's llm.Resolve target and claude-haiku-5-5 (thinking off) answer every (ask, reply) pair.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

var prompts = []string{
	"A voice robot just answered a person. Reply YES if the robot's reply asks the person a question or invites them to answer; otherwise reply NO. Reply with one word: YES or NO.",
	"Should the robot listen for the person's answer now? Say YES only if the robot's reply ends by asking the person something or asking them to say something (an answer, a choice, a guess). Say NO if the reply is complete and expects nothing back. Answer YES or NO only.",
}

type pair struct {
	l, ask, reply string
	yes           bool
}

var pairs = []pair{
	{lang.English, "ask me a riddle", "What has keys but can't open a single lock?", true},
	{lang.English, "I'm bored", "Want to play a game? I can quiz you on animals or tell you a joke.", true},
	{lang.English, "what should I cook tonight", "How about pasta with garlic and tomatoes? Do you have those at home?", true},
	{lang.English, "what time is it", "It's half past seven in the evening.", false},
	{lang.English, "tell me a fact about the moon", "The Moon drifts about four centimetres further from Earth every year.", false},
	{lang.Estonian, "küsi minult mõistatus", "Mis see on: hommikul neljal jalal, lõunal kahel ja õhtul kolmel?", true},
	{lang.Estonian, "mul on igav", "Kas mängime midagi? Võin sulle loomade kohta küsimusi esitada.", true},
	{lang.Estonian, "mida ma täna õhtul süüa võiksin", "Kuidas oleks pastaga? Kas sul on kodus tomateid?", true},
	{lang.Estonian, "mis kell on", "Kell on pool kaheksa õhtul.", false},
	{lang.Estonian, "räägi mulle midagi kuust", "Kuu kaugeneb Maast igal aastal umbes neli sentimeetrit.", false},
}

// noThinking adds Claude's `"thinking":{"type":"disabled"}` (go-openai has no field for it; llm-trials.md (docs/)).
type noThinking struct{}

func (noThinking) RoundTrip(r *http.Request) (*http.Response, error) {
	var m map[string]any
	b, err := io.ReadAll(r.Body)
	if err == nil && json.Unmarshal(b, &m) == nil {
		m["thinking"] = map[string]string{"type": "disabled"}
		b, _ = json.Marshal(m)
	}
	r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
	return http.DefaultTransport.RoundTrip(r)
}

type target struct {
	name  string
	t     llm.Target
	langs []string // languages whose pairs count toward this brain's verdict
}

func client(t llm.Target) *openai.Client {
	cfg := openai.DefaultConfig(t.Key)
	cfg.BaseURL = t.BaseURL
	if !t.Ollama() {
		cfg.HTTPClient = &http.Client{Transport: noThinking{}}
	}
	return openai.NewClientWithConfig(cfg)
}

func ask(c *openai.Client, t llm.Target, sys, user string) (string, time.Duration, error) {
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sys}, {Role: openai.ChatMessageRoleUser, Content: user}}}
	if t.Ollama() {
		r.ReasoningEffort, r.MaxTokens = "none", 8
	} else {
		r.MaxCompletionTokens = 8
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	res, err := c.CreateChatCompletion(ctx, r)
	d := time.Since(start)
	if err != nil || len(res.Choices) == 0 {
		return "", d, fmt.Errorf("no answer: %v", err)
	}
	return strings.TrimSpace(res.Choices[0].Message.Content), d, nil
}

func yes(s string) bool { return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "YES") }

func main() {
	cfgPath := flag.String("config", "myvectorConfig.json", "myvectorConfig.json to read the LLM profiles and Claude key from")
	also := flag.String("also", llm.DefaultETModel, "comma-separated extra Ollama models, scored as et-EE candidates")
	flag.Parse()
	lang.Path = func() string { return *cfgPath }
	c := llm.Load()
	if c.Profiles == nil {
		fmt.Fprintln(os.Stderr, "no llm profiles in", *cfgPath)
		os.Exit(1)
	}
	var ts []target
	byModel := map[string]int{}
	for _, l := range []string{lang.English, lang.Estonian} {
		t, ok := llm.Resolve(c, l, llm.KG{})
		if !ok || !t.Ollama() {
			continue
		}
		if i, seen := byModel[t.Model]; seen {
			ts[i].langs = append(ts[i].langs, l)
			continue
		}
		byModel[t.Model] = len(ts)
		ts = append(ts, target{"ollama " + t.Model, t, []string{l}})
	}
	_, base := llm.Effective(c, llm.KG{})
	for _, m := range strings.Split(*also, ",") {
		if _, seen := byModel[m]; m != "" && !seen {
			byModel[m] = len(ts)
			ts = append(ts, target{"ollama " + m, llm.Target{Provider: llm.Ollama, BaseURL: base + "/v1", Key: "ollama", Model: m}, []string{lang.Estonian}})
		}
	}
	if c.ClaudeKey != "" {
		ts = append(ts, target{"claude claude-haiku-5-5", llm.Target{Provider: llm.Claude, BaseURL: llm.AnthropicBase + "/v1", Key: c.ClaudeKey, Model: "claude-haiku-5-5"},
			[]string{lang.English, lang.Estonian}})
	}
	for pi, sys := range prompts {
		fmt.Printf("prompt %d\n", pi+1)
		for _, tg := range ts {
			cl := client(tg.t)
			if _, _, err := ask(cl, tg.t, sys, "warm up"); err != nil {
				fmt.Printf("  %-34s error: %v\n", tg.name, err)
				continue
			}
			var ms []time.Duration
			score := map[string][2]int{} // lang → {correct, total}
			for _, p := range pairs {
				got, d, err := ask(cl, tg.t, sys, "Person: "+p.ask+"\nRobot: "+p.reply)
				ms = append(ms, d)
				s := score[p.l]
				s[1]++
				if err == nil && yes(got) == p.yes {
					s[0]++
				} else {
					fmt.Printf("    miss %s %q → %q %v\n", p.l, p.reply, got, err)
				}
				score[p.l] = s
			}
			sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
			fmt.Printf("  %-34s en %d/%d  et %d/%d  median %4d ms  max %4d ms  counts for %v\n", tg.name,
				score[lang.English][0], score[lang.English][1], score[lang.Estonian][0], score[lang.Estonian][1],
				ms[len(ms)/2].Milliseconds(), ms[len(ms)-1].Milliseconds(), tg.langs)
		}
	}
}
