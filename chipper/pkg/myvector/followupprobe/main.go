// Command followupprobe scores candidate "listen again?" prompts (follow-up-listening.md (docs/plans/) Stage 1.1).
//
//	go run ./pkg/myvector/followupprobe [-config ../../../my-vector/secrets/server/myvectorConfig.json] [-models a,b -think none,low -think-budget N]   # from chipper/
//
// Each language's llm.Resolve target and claude-haiku-5-5 (thinking off), or each -models entry on its own, answer every (ask, reply) pair per -think arm.
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
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	openai "github.com/sashabaranov/go-openai"
)

// replyCap is the YES/NO reply's max tokens before any thinking budget.
const replyCap = 8

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
	b, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		m["thinking"] = map[string]string{"type": "disabled"}
		if b, err = json.Marshal(m); err != nil {
			return nil, err
		}
	}
	r = r.Clone(r.Context())
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	r.Body, _ = r.GetBody()
	r.ContentLength = int64(len(b))
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

// request is one pair call: Ollama gets th.Apply with replyCap; Claude ignores th.
func request(t llm.Target, sys, user string, th llm.Thinking) openai.ChatCompletionRequest {
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sys}, {Role: openai.ChatMessageRoleUser, Content: user}}}
	if t.Ollama() {
		th.Apply(&r, replyCap)
	} else {
		llm.SamplingFor(t.Provider, t.Model).Apply(&r)
		r.MaxCompletionTokens = replyCap
	}
	return r
}

// call sends r: Ollama through probekit.Chat (keeps reasoning), Claude through go-openai.
func call(c *openai.Client, t llm.Target, r openai.ChatCompletionRequest) (probekit.Call, error) {
	if t.Ollama() {
		return probekit.Chat(t.BaseURL, r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	start := time.Now()
	res, err := c.CreateChatCompletion(ctx, r)
	d := time.Since(start)
	if err != nil || len(res.Choices) == 0 {
		return probekit.Call{Dur: d}, fmt.Errorf("no answer: %v", err)
	}
	ch := res.Choices[0]
	return probekit.Call{Reply: strings.TrimSpace(ch.Message.Content), Finish: string(ch.FinishReason),
		Prompt: res.Usage.PromptTokens, Completion: res.Usage.CompletionTokens, ReplyTok: res.Usage.CompletionTokens, Dur: d}, nil // Claude: no thinking
}

func yes(s string) bool { return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "YES") }

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read the LLM profiles and Claude key from")
	also := flag.String("also", llm.DefaultETModel, "comma-separated extra Ollama models, scored as et-EE candidates")
	models := flag.String("models", "", "comma-separated Ollama models, each its own verdict on en+et, instead of the saved profiles, -also and Claude")
	thinkF := flag.String("think", "none", "thinking arms, comma-separated: none, low, medium, high")
	budget := flag.Int("think-budget", 0, "thinking budget in tokens for every arm but none (0 = llm table)")
	flag.Parse()
	levels, err := probekit.ParseThink(*thinkF)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	think := probekit.Arms(levels, *budget)
	multi := len(think) > 1
	lang.Path = func() string { return *cfgPath }
	c := llm.Load()
	_, base := llm.Effective(c, llm.KG{})
	ollama := func(m string) llm.Target {
		return llm.Target{Provider: llm.Ollama, BaseURL: base + "/v1", Key: "ollama", Model: m}
	}
	var ts []target
	byModel := map[string]int{}
	if *models != "" {
		for _, m := range strings.Split(*models, ",") {
			m = strings.TrimSpace(m)
			if _, seen := byModel[m]; m == "" || seen {
				fmt.Fprintln(os.Stderr, "-models: empty or repeated entry")
				os.Exit(2)
			}
			byModel[m] = len(ts)
			ts = append(ts, target{"ollama " + m, ollama(m), []string{lang.English, lang.Estonian}})
		}
	} else {
		if c.Profiles == nil {
			fmt.Fprintln(os.Stderr, "no llm profiles in", *cfgPath)
			os.Exit(1)
		}
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
		for _, m := range strings.Split(*also, ",") {
			m = strings.TrimSpace(m)
			if _, seen := byModel[m]; m != "" && !seen {
				byModel[m] = len(ts)
				ts = append(ts, target{"ollama " + m, ollama(m), []string{lang.Estonian}})
			}
		}
		if c.ClaudeKey != "" {
			ts = append(ts, target{"claude claude-haiku-5-5", llm.Target{Provider: llm.Claude, BaseURL: llm.AnthropicBase + "/v1", Key: c.ClaudeKey, Model: "claude-haiku-5-5"},
				[]string{lang.English, lang.Estonian}})
		}
	}
	cls := make([]*openai.Client, len(ts))
	for i, tg := range ts {
		cls[i] = client(tg.t)
	}
	for pi, sys := range prompts {
		fmt.Printf("prompt %d\n", pi+1)
		for i, tg := range ts {
			none := probekit.Arm{Level: llm.None}.Thinking(tg.t.Model, llm.PathFollowUp)
			if _, err := call(cls[i], tg.t, request(tg.t, sys, "warm up", none)); err != nil {
				fmt.Printf("  %-34s error: %v\n", tg.name, err)
				continue
			}
			var tas []probekit.Arm // Claude has no thinking arms: it runs the none arm only
			for _, a := range think {
				if tg.t.Ollama() || a.Level == llm.None {
					tas = append(tas, a)
				}
			}
			pre := func(a probekit.Arm) string {
				if !multi {
					return ""
				}
				return a.Label(tg.t.Model) + " "
			}
			ms := make([][]time.Duration, len(tas))
			calls := make([][]probekit.Call, len(tas))
			score := make([]map[string][2]int, len(tas)) // lang → {correct, total}
			for k := range tas {
				score[k] = map[string][2]int{}
			}
			for j, p := range pairs {
				for _, k := range probekit.Order(len(tas), j) {
					a := tas[k]
					start := time.Now()
					got, err := call(cls[i], tg.t, request(tg.t, sys, "Person: "+p.ask+"\nRobot: "+p.reply, a.Thinking(tg.t.Model, llm.PathFollowUp)))
					ms[k] = append(ms[k], time.Since(start))
					if err == nil {
						calls[k] = append(calls[k], got)
					}
					s := score[k][p.l]
					s[1]++
					if err == nil && yes(got.Reply) == p.yes {
						s[0]++
					} else {
						fmt.Printf("    %smiss %s %q → %q %v\n", pre(a), p.l, p.reply, got.Reply, err)
					}
					score[k][p.l] = s
				}
			}
			for k, a := range tas {
				sort.Slice(ms[k], func(i, j int) bool { return ms[k][i] < ms[k][j] })
				sc := score[k]
				fmt.Printf("  %s%-34s en %d/%d  et %d/%d  median %4d ms  max %4d ms  counts for %v\n", pre(a), tg.name,
					sc[lang.English][0], sc[lang.English][1], sc[lang.Estonian][0], sc[lang.Estonian][1],
					ms[k][len(ms[k])/2].Milliseconds(), ms[k][len(ms[k])-1].Milliseconds(), tg.langs)
				fmt.Println("    " + probekit.Summarize(calls[k]).Line(a.Label(tg.t.Model)))
			}
		}
	}
}
