// Command searchbench scores each search provider on fixed Estonian queries (web-search plan W11, Stage 3.1).
//
//	go run ./pkg/myvector/searchbench [-config myvectorConfig.json] [-providers tavily,ollama,searxng] [-judge claude-haiku-5-5]   # from chipper/
//
// Every call spends quota via search.State.Take; without claude_key the judge columns print "-".
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
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
)

var queries = []string{
	"mis on euro ja dollari kurss täna?",
	"kes on Eesti peaminister?",
	"mis juhtus täna Tallinnas?",
	"kui palju maksab bitcoin?",
	"millal on Tartu Lõunakeskus lahti?",
	"kes on Soome president?",
	"mis on elektri börsihind täna?",
	"kes võitis viimase Eesti Laulu?",
	"Eesti uudised täna",
	"kes võitis eile Eesti jalgpalli meistriliigas?",
}

const judgePrompt = `You grade web search results for a voice robot that answers Estonian questions.
Reply with JSON only: {"relevance": R, "et": E}
R (0-3): 0 = nothing useful, 1 = on topic but no answer, 2 = answer present but stale or partial, 3 = a current, direct answer.
E (0-1): 1 if most results are in Estonian or about Estonia as asked, else 0.`

type score struct{ Relevance, ET int }

// parseScore reads the judge's {"relevance","et"} reply, code fences and prose around it allowed.
func parseScore(reply string) (score, error) {
	i, j := strings.IndexByte(reply, '{'), strings.LastIndexByte(reply, '}')
	if i < 0 || j < i {
		return score{}, fmt.Errorf("no JSON object in %q", reply)
	}
	var s struct {
		Relevance *int `json:"relevance"`
		ET        *int `json:"et"`
	}
	if err := json.Unmarshal([]byte(reply[i:j+1]), &s); err != nil {
		return score{}, err
	}
	if s.Relevance == nil || s.ET == nil {
		return score{}, errors.New("relevance and et required")
	}
	if *s.Relevance < 0 || *s.Relevance > 3 || *s.ET < 0 || *s.ET > 1 {
		return score{}, fmt.Errorf("score out of range: relevance %d, et %d", *s.Relevance, *s.ET)
	}
	return score{*s.Relevance, *s.ET}, nil
}

func judge(key, model, query string, rs []search.Result) (score, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nResults:\n", query)
	for i, r := range rs {
		fmt.Fprintf(&b, "%d. %s (%s) %s\n%s\n", i+1, r.Title, r.URL, r.Date, r.Snippet)
	}
	body, _ := json.Marshal(map[string]any{
		"model": model, "max_tokens": 64, "system": judgePrompt,
		"messages": []map[string]string{{"role": "user", "content": b.String()}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, llm.AnthropicBase+"/v1/messages", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", llm.AnthropicVersion)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return score{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return score{}, fmt.Errorf("judge HTTP %d", res.StatusCode)
	}
	var out struct {
		Content []struct{ Text string } `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Content) == 0 {
		return score{}, fmt.Errorf("judge reply: %v", err)
	}
	return parseScore(out.Content[0].Text)
}

type row struct {
	name          string
	relevance, et float64 // means over all queries; an errored query scores 0
	judged        bool
	medianMS      int64
	errors        int
}

// pick applies W11: median over search.Timeout disqualifies, then highest relevance, et, lower median ms.
func pick(rows []row) (string, bool) {
	var ok []row
	for _, r := range rows {
		if r.judged && r.medianMS <= search.Timeout.Milliseconds() {
			ok = append(ok, r)
		}
	}
	if len(ok) == 0 {
		return "", false
	}
	sort.SliceStable(ok, func(i, j int) bool {
		a, b := ok[i], ok[j]
		if a.relevance != b.relevance {
			return a.relevance > b.relevance
		}
		if a.et != b.et {
			return a.et > b.et
		}
		return a.medianMS < b.medianMS
	})
	return ok[0].name, true
}

func median(ms []int64) int64 {
	if len(ms) == 0 {
		return 0
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	return ms[len(ms)/2]
}

func bench(p search.Provider, st *search.State, key, model string) row {
	r := row{name: p.Name(), judged: key != ""}
	var ms []int64
	for _, q := range queries {
		if !st.Take(p.Name(), time.Now()) {
			r.errors++
			fmt.Fprintf(os.Stderr, "%s: capped\n", p.Name())
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		rs, err := p.Search(ctx, q, lang.Estonian, search.SnippetsClaude)
		cancel()
		ms = append(ms, time.Since(start).Milliseconds())
		if err == nil && len(rs) == 0 {
			err = errors.New("no results")
		}
		if err != nil {
			r.errors++
			fmt.Fprintf(os.Stderr, "%s %q: %v\n", p.Name(), q, err)
			continue
		}
		if !r.judged {
			continue
		}
		s, err := judge(key, model, q, rs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "judge %s %q: %v\n", p.Name(), q, err)
			continue
		}
		r.relevance += float64(s.Relevance) / float64(len(queries))
		r.et += float64(s.ET) / float64(len(queries))
	}
	r.medianMS = median(ms)
	return r
}

func main() {
	config := flag.String("config", "myvectorConfig.json", "myvectorConfig.json with the search keys (myvectorSearch.json beside it holds the quota)")
	names := flag.String("providers", strings.Join(search.DefaultOrder, ","), "comma-separated providers")
	model := flag.String("judge", "claude-haiku-5-5", "Claude judge model")
	flag.Parse()
	lang.Path = func() string { return *config }
	cfg, key := search.LoadConfig(), llm.Load().ClaudeKey
	if key == "" {
		fmt.Fprintln(os.Stderr, "no claude_key: judge skipped")
	}
	st := search.Open(search.StatePath())
	var rows []row
	for _, n := range strings.Split(*names, ",") {
		p, ok := cfg.ProviderFor(strings.TrimSpace(n))
		if !ok {
			fmt.Fprintf(os.Stderr, "%s: no key/URL or unknown, skipped\n", n)
			continue
		}
		rows = append(rows, bench(p, st, key, *model))
	}
	fmt.Println("provider | relevance | et | median ms | errors")
	for _, r := range rows {
		rel, et := "-", "-"
		if r.judged {
			rel, et = fmt.Sprintf("%.2f", r.relevance), fmt.Sprintf("%.2f", r.et)
		}
		fmt.Printf("%s | %s | %s | %d | %d\n", r.name, rel, et, r.medianMS, r.errors)
	}
	if p, ok := pick(rows); ok {
		fmt.Println("pick:", p)
	}
}
