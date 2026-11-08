// Command visionbench scores local vision models on robot photos (2026-11-08-vision-setting.md (docs/plans/) V12, V13).
//
//	go run ./pkg/myvector/visionbench [-config myvectorConfig.json -dir ../robot/work/vision-bench -lang et-EE -models a,b -show]   # from chipper/
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
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

func die(msg string) {
	fmt.Fprintln(os.Stderr, "visionbench: "+msg)
	os.Exit(2)
}

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read the Ollama URL from")
	dir := flag.String("dir", "../robot/work/vision-bench", "photos + checklist.json")
	langF := flag.String("lang", "", "only this language: en-US or et-EE")
	models := flag.String("models", "gemma4:e4b-it-qat,qwen3.5:4b", "comma-separated Ollama vision models")
	show := flag.Bool("show", false, "print every photo's answers side by side")
	flag.Parse()
	langs := []string{lang.English, lang.Estonian}
	if *langF != "" {
		if _, ok := question[*langF]; !ok {
			die("-lang: en-US or et-EE")
		}
		langs = []string{*langF}
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
	_, base := llm.Effective(llm.Load(), llm.KG{})
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
	answers := map[string][]string{} // model|lang → per photo
	fmt.Println("model | lang | keywords % | median ms | errors")
	for _, m := range ms {
		for _, l := range langs {
			sys := prompt.Render(prompt.Default(prompt.Ollama), prompt.Sample(l))
			var sum float64
			var durs []time.Duration
			errs := 0
			got := make([]string, len(items))
			for i, it := range items {
				c, err := probekit.Chat(base+"/v1", request(m, sys, question[l], photos[i]))
				if err != nil {
					errs++
					got[i] = "ERROR: " + err.Error()
					continue
				}
				got[i] = c.Reply
				durs = append(durs, c.Dur)
				sum += score(c.Reply, it.words(l))
			}
			answers[m+"|"+l] = got
			fmt.Printf("%s | %s | %.0f | %d | %d\n", m, l, 100*sum/float64(len(items)), median(durs).Milliseconds(), errs)
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
