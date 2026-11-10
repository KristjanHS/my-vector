// Command visionbench scores local vision models on robot photos (2026-11-08-vision-setting.md (docs/plans/) V12, V13);
// -arms compares et-EE prompts (2026-11-10-vision-et-prompt-bench.md (docs/plans/) B1–B9).
//
//	go run ./pkg/myvector/visionbench [-config myvectorConfig.json -dir ../robot/work/vision-bench -lang et-EE -models a,b -arms a,b,c -show]   # from chipper/
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/relay"
	"github.com/sashabaranov/go-openai"
)

// item is one checklist.json row (V11).
type item struct {
	Photo string   `json:"photo"`
	ET    []string `json:"et"`
	EN    []string `json:"en"`
}

func (it item) words(l string) []string {
	if l == lang.Estonian {
		return it.ET
	}
	return it.EN
}

var question = map[string]string{lang.English: "What do you see?", lang.Estonian: "Mida sa näed?"}

// loadChecklist reads dir/checklist.json; every photo must exist and every word list be non-empty.
func loadChecklist(dir string) ([]item, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "checklist.json"))
	if err != nil {
		return nil, err
	}
	var items []item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("checklist.json: %v", err)
	}
	if len(items) == 0 {
		return nil, errors.New("checklist.json: no photos")
	}
	for _, it := range items {
		if _, err := os.Stat(filepath.Join(dir, it.Photo)); err != nil {
			return nil, fmt.Errorf("checklist.json: photo %q: %v", it.Photo, err)
		}
		if len(it.ET) == 0 || len(it.EN) == 0 {
			return nil, fmt.Errorf("checklist.json: photo %q needs non-empty et and en word lists", it.Photo)
		}
		for _, w := range append(append([]string{}, it.ET...), it.EN...) {
			if w == "" || strings.IndexFunc(w, notWordRune) >= 0 {
				return nil, fmt.Errorf("checklist.json: photo %q: word %q must be one word of letters/digits", it.Photo, w)
			}
		}
	}
	return items, nil
}

func notWordRune(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// score is the share of words some answer token starts with, case-folded (V13).
func score(answer string, words []string) float64 {
	if len(words) == 0 {
		return 0
	}
	toks := strings.FieldsFunc(strings.ToLower(answer), notWordRune)
	hit := 0
	for _, w := range words {
		w = strings.ToLower(w)
		for _, t := range toks {
			if strings.HasPrefix(t, w) {
				hit++
				break
			}
		}
	}
	return float64(hit) / float64(len(words))
}

func request(model, sys, q string, jpeg []byte) openai.ChatCompletionRequest {
	r := openai.ChatCompletionRequest{Model: model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sys},
		{Role: openai.ChatMessageRoleUser, MultiContent: []openai.ChatMessagePart{
			{Type: openai.ChatMessagePartTypeText, Text: q},
			{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &openai.ChatMessageImageURL{
				URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg), Detail: openai.ImageURLDetailLow}},
		}},
	}}
	llm.ThinkingFor(llm.Ollama, model, llm.PathChat, llm.None).Apply(&r, prompt.ReplyCap(prompt.Ollama))
	return r
}

// unload frees model's VRAM before the next one loads (keep_alive 0, as ttr forkUnload).
func unload(base, model string) {
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Post(base+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "unload "+model+": "+err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unload "+model+": "+resp.Status)
	}
}

func median(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

// armSystem is arm a's (et persona, B1) or b's (English persona + "Answer in Estonian", B2) system prompt.
func armSystem(arm string) string {
	brain := prompt.OllamaEt
	if arm == "b" {
		brain = prompt.Ollama
	}
	return prompt.Render(prompt.Default(brain), prompt.Sample(brain, lang.Estonian))
}

// relayRequest is arm c's hop 2 (B4): hop 1's English answer, text only, to the saved et-EE brain t.
func relayRequest(t llm.Target, hop1 string) openai.ChatCompletionRequest {
	brain := prompt.Claude
	if t.Provider == llm.Ollama {
		brain = prompt.OllamaFor(lang.Estonian)
	}
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: prompt.Render(prompt.Default(brain), prompt.Sample(brain, lang.Estonian))},
		{Role: openai.ChatMessageRoleUser, Content: "Kaamera nägi (inglise keeles): " + hop1 + "\n\n" + question[lang.Estonian]},
	}}
	llm.ThinkingFor(t.Provider, t.Model, llm.PathChat, llm.None).Apply(&r, prompt.ReplyCap(brain))
	return r
}

// timedChat is one hop-2 call on cl, wall-clock timed like probekit.Chat (120 s cap).
func timedChat(cl *openai.Client, r openai.ChatCompletionRequest) (string, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	t0 := time.Now()
	resp, err := cl.CreateChatCompletion(ctx, r)
	d := time.Since(t0)
	if err != nil {
		return "", d, err
	}
	if len(resp.Choices) == 0 {
		return "", d, errors.New("no choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), d, nil
}

// result is one model × arm pass over the photos.
type result struct {
	got  []string
	durs []time.Duration
	sum  float64
	errs int
}

func (r *result) add(i int, reply string, d time.Duration, err error, words []string) {
	if err != nil {
		r.errs++
		r.got[i] = "ERROR: " + err.Error()
		return
	}
	r.got[i] = reply
	r.durs = append(r.durs, d)
	r.sum += score(reply, words)
}

func (r *result) row(model, label string) {
	fmt.Printf("%s | %s | %.0f | %d | %d\n", model, label, 100*r.sum/float64(len(r.got)), median(r.durs).Milliseconds(), r.errs)
}

// photoRun asks model m about every photo with system sys in language l, scoring on l's words.
func photoRun(base, m, sys, l string, items []item, photos [][]byte) *result {
	r := &result{got: make([]string, len(items))}
	for i, it := range items {
		c, err := probekit.Chat(base+"/v1", request(m, sys, question[l], photos[i]))
		r.add(i, c.Reply, c.Dur, err, it.words(l))
	}
	return r
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "visionbench: "+msg)
	os.Exit(2)
}

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read the Ollama URL from")
	dir := flag.String("dir", "../robot/work/vision-bench", "photos + checklist.json")
	langF := flag.String("lang", "", "only this language: en-US or et-EE")
	models := flag.String("models", llm.DefaultVisionET, "comma-separated Ollama vision models (gemma4 is out: llm.NoVision)")
	armsF := flag.String("arms", "", "et-EE prompt arms to compare, any of a,b,c,d (default: one run per language)")
	show := flag.Bool("show", false, "print every photo's answers side by side")
	flag.Parse()
	langs := []string{lang.English, lang.Estonian}
	if *langF != "" {
		if _, ok := question[*langF]; !ok {
			die("-lang: en-US or et-EE")
		}
		langs = []string{*langF}
	}
	var arms []string
	for _, a := range strings.Split(*armsF, ",") {
		switch a = strings.TrimSpace(a); a {
		case "":
		case "a", "b", "c", "d":
			arms = append(arms, a)
		default:
			die("-arms: any of a,b,c,d")
		}
	}
	if len(arms) > 0 && *langF == lang.English {
		die("-arms compares et-EE prompts; drop -lang en-US")
	}
	items, err := loadChecklist(*dir)
	if err != nil {
		die(err.Error())
	}
	photos := make([][]byte, len(items))
	for i, it := range items {
		if photos[i], err = os.ReadFile(filepath.Join(*dir, it.Photo)); err != nil {
			die(err.Error())
		}
	}
	if _, err := os.Stat(*cfgPath); err != nil {
		die("-config: " + err.Error()) // llm.Load would silently fall back to DefaultOllamaURL
	}
	lang.Path = func() string { return *cfgPath }
	cfg := llm.Load()
	_, base := llm.Effective(cfg, llm.KG{})
	fmt.Println("ollama " + base + ", " + fmt.Sprint(len(items)) + " photos; keywords % counts an errored photo as 0")
	var ms []string
	for _, m := range strings.Split(*models, ",") {
		if m = strings.TrimSpace(m); m != "" {
			ms = append(ms, m)
		}
	}
	if len(ms) == 0 {
		die("-models: at least one model")
	}
	if len(arms) > 0 {
		runArms(cfg, base, ms, arms, items, photos, *show)
		return
	}
	answers := map[string][]string{} // model|lang → per photo
	fmt.Println("model | lang | keywords % | median ms | errors")
	for _, m := range ms {
		for _, l := range langs {
			sys := prompt.Render(prompt.Default(prompt.OllamaFor(l)), prompt.Sample(prompt.OllamaFor(l), l))
			r := photoRun(base, m, sys, l, items, photos)
			answers[m+"|"+l] = r.got
			r.row(m, l)
		}
		unload(base, m)
	}
	if !*show {
		return
	}
	for i, it := range items {
		for _, l := range langs {
			fmt.Printf("\n%s %s (want %s)\n", it.Photo, l, strings.Join(it.words(l), ", "))
			for _, m := range ms {
				fmt.Printf("  %-22s %s\n", m+":", strings.ReplaceAll(answers[m+"|"+l][i], "\n", " "))
			}
		}
	}
}

// runArms is the et-EE prompt bench: a and b per B1/B2; c (relay) and d (translate) send the en-US run through the
// saved et-EE brain, cold then warm per photo (B3, B4, B9, B10).
func runArms(cfg llm.Config, base string, ms, arms []string, items []item, photos [][]byte, show bool) {
	hop2 := map[string]func(llm.Target, string) openai.ChatCompletionRequest{"c": relayRequest, "d": relay.Translate}
	twoHop := slices.Contains(arms, "c") || slices.Contains(arms, "d")
	var brain llm.Target
	if twoHop {
		var ok bool
		if brain, ok = llm.Resolve(cfg, lang.Estonian, llm.KG{}); !ok {
			die("-arms c/d: no saved et-EE brain profile")
		}
		fmt.Println("relay brain " + brain.Provider + " " + brain.Model)
	}
	var labels []string
	for _, a := range arms {
		if labels = append(labels, a); hop2[a] != nil {
			labels = append(labels, a+"-warm")
		}
	}
	if twoHop {
		labels = append(labels, "hop1 (en)")
	}
	answers := map[string][]string{} // model|label → per photo
	fmt.Println("model | arm | keywords % | median ms | errors")
	for _, m := range ms {
		res := map[string]*result{}
		for _, a := range arms {
			if hop2[a] == nil {
				res[a] = photoRun(base, m, armSystem(a), lang.Estonian, items, photos)
			}
		}
		if twoHop {
			sys := prompt.Render(prompt.Default(prompt.Ollama), prompt.Sample(prompt.Ollama, lang.English))
			res["hop1 (en)"] = photoRun(base, m, sys, lang.English, items, photos)
		}
		unload(base, m)
		for _, a := range arms {
			if hop2[a] == nil {
				continue
			}
			hop1 := res["hop1 (en)"]
			cold := &result{got: make([]string, len(items))}
			warm := &result{got: make([]string, len(items))}
			cl := llm.NewClient(brain)
			d1 := 0
			for i, it := range items {
				if strings.HasPrefix(hop1.got[i], "ERROR: ") {
					cold.add(i, "", 0, errors.New("hop 1 failed"), nil)
					warm.add(i, "", 0, errors.New("hop 1 failed"), nil)
					continue
				}
				h1 := hop1.durs[d1]
				d1++
				if brain.Provider == llm.Ollama {
					unload(brain.OllamaBase(), brain.Model)
				}
				req := hop2[a](brain, hop1.got[i])
				reply, d, err := timedChat(cl, req)
				if a == "d" {
					reply = relay.Attach(hop1.got[i], reply)
				}
				cold.add(i, reply, h1+d, err, it.words(lang.Estonian))
				reply, d, err = timedChat(cl, req)
				if a == "d" {
					reply = relay.Attach(hop1.got[i], reply)
				}
				warm.add(i, reply, h1+d, err, it.words(lang.Estonian))
			}
			if brain.Provider == llm.Ollama {
				unload(brain.OllamaBase(), brain.Model)
			}
			res[a], res[a+"-warm"] = cold, warm
		}
		for _, l := range labels {
			res[l].row(m, l)
			answers[m+"|"+l] = res[l].got
		}
	}
	if !show {
		return
	}
	for i, it := range items {
		fmt.Printf("\n%s et-EE (want %s)\n", it.Photo, strings.Join(it.words(lang.Estonian), ", "))
		for _, m := range ms {
			for _, l := range labels {
				if !strings.HasSuffix(l, "-warm") {
					fmt.Printf("  %-30s %s\n", m+" "+l+":", strings.ReplaceAll(answers[m+"|"+l][i], "\n", " "))
				}
			}
		}
	}
}
