// Command searchbench scores each search provider on fixed Estonian queries (web-search plan W11, Stage 3.1).
//
//	go run ./pkg/myvector/searchbench [-config ../secrets/server/myvectorConfig.json] [-providers tavily,searxng] [-judge claude-haiku-5-5] [-news]   # from chipper/
//
// -news scores 5 spoken news asks per query arm instead (search-news-quality N9).
//
// Every call spends quota via search.State.Take; without claude_key the judge columns print "-".
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
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

// newsAsks are spoken et news asks, verbatim (search-news-quality N9).
var newsAsks = []string{
	"otsin pooljuhtide teemalisi finantsuudiseid, mis mõjutavad aktsiaturge",
	"otsi uudiseid Nvidia aktsia kohta",
	"mis on uudist tehisintellekti kiipide kohta",
	"otsi värskeid uudiseid LHV aktsia kohta",
	"mis juhtus täna börsil",
}

// arm is one query shape: verbatim, N1 keywords, N1+N3 news mode, N1+N3+N7 English (SearXNG only, Q3/Q4).
type arm struct {
	name      string
	q         func(ask string) search.Query
	onlySearx bool
}

var arms = []arm{
	{"verbatim", func(a string) search.Query { return search.Query{Text: a} }, false},
	{"N1", func(a string) search.Query { return search.Query{Text: search.Keywords(a)} }, false},
	{"N1+N3", func(a string) search.Query { return search.Query{Text: search.Keywords(a), News: search.IsNews(a)} }, false},
	{"N1+N3+N7", func(a string) search.Query {
		q := search.Query{Text: search.Keywords(a), News: search.IsNews(a)}
		if q.News {
			q.Text = search.English(q.Text)
		}
		return q
	}, true},
}

// gateWords are the words production's N6 gate (search.look) matches results against: the keywords plus English.
func gateWords(ask string) string {
	k := search.Keywords(ask)
	return k + " " + search.English(k)
}

const newsJudgePrompt = `You grade web search results for a voice robot that answers a spoken Estonian news ask. Today is %s.
Reply with JSON only: {"relevance": R, "dated": D}
R (0-3): 0 = nothing useful, 1 = on topic but no news, 2 = on-topic news but stale or partial, 3 = current on-topic news.
D (0-1): 1 if most results carry a date within the past 7 days, else 0.`

const judgePrompt = `You grade web search results for a voice robot that answers Estonian questions.
Reply with JSON only: {"relevance": R, "et": E}
R (0-3): 0 = nothing useful, 1 = on topic but no answer, 2 = answer present but stale or partial, 3 = a current, direct answer.
E (0-1): 1 if most results are in Estonian or about Estonia as asked, else 0.`

type score struct{ Relevance, ET, Dated int }

// parseScore reads the judge's {"relevance","et"} reply ({"relevance","dated"} when news), code fences and prose
// around it allowed.
func parseScore(reply string, news bool) (score, error) {
	i, j := strings.IndexByte(reply, '{'), strings.LastIndexByte(reply, '}')
	if i < 0 || j < i {
		return score{}, fmt.Errorf("no JSON object in %q", reply)
	}
	var s struct {
		Relevance *int `json:"relevance"`
		ET        *int `json:"et"`
		Dated     *int `json:"dated"`
	}
	if err := json.Unmarshal([]byte(reply[i:j+1]), &s); err != nil {
		return score{}, err
	}
	second, name := s.ET, "et"
	if news {
		second, name = s.Dated, "dated"
	}
	if s.Relevance == nil || second == nil {
		return score{}, fmt.Errorf("relevance and %s required", name)
	}
	if *s.Relevance < 0 || *s.Relevance > 3 || *second < 0 || *second > 1 {
		return score{}, fmt.Errorf("score out of range: relevance %d, %s %d", *s.Relevance, name, *second)
	}
	if news {
		return score{Relevance: *s.Relevance, Dated: *second}, nil
	}
	return score{Relevance: *s.Relevance, ET: *second}, nil
}

func judge(key, model, query string, rs []search.Result, news bool) (score, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nResults:\n", query)
	for i, r := range rs {
		fmt.Fprintf(&b, "%d. %s (%s) %s\n%s\n", i+1, r.Title, r.URL, r.Date, r.Snippet)
	}
	sys := judgePrompt
	if news {
		sys = fmt.Sprintf(newsJudgePrompt, time.Now().Format("2006-01-02"))
	}
	reply, err := probekit.Claude(key, model, sys, b.String(), 200)
	if err != nil {
		return score{}, err
	}
	return parseScore(reply, news)
}

type row struct {
	name, arm            string
	relevance, et, dated float64 // means over all queries; an errored query scores 0
	judged               bool
	medianMS             int64
	errors               int
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

// bench runs every ask through p as a's query (nil a: the ask verbatim, general mode).
func bench(p search.Provider, st *search.State, key, model string, asks []string, a *arm) row {
	r := row{name: p.Name(), judged: key != ""}
	if a != nil {
		r.arm = a.name
	}
	var ms []int64
	for _, q := range asks {
		sq := search.Query{Text: q}
		if a != nil {
			sq = a.q(q)
		}
		if !st.Take(p.Name(), time.Now()) {
			r.errors++
			fmt.Fprintf(os.Stderr, "%s %s: capped\n", p.Name(), r.arm)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		start := time.Now()
		rs, err := p.Search(ctx, sq, lang.Estonian, search.SnippetsClaude)
		cancel()
		ms = append(ms, time.Since(start).Milliseconds())
		if err == nil && a != nil && a.name != "verbatim" {
			rs = search.Relevant(rs, gateWords(q)) // score what production keeps (N6)
		}
		if err == nil && len(rs) == 0 {
			err = errors.New("no results")
		}
		if err != nil {
			r.errors++
			fmt.Fprintf(os.Stderr, "%s %s %q: %v\n", p.Name(), r.arm, q, err)
			continue
		}
		if !r.judged {
			continue
		}
		s, err := judge(key, model, q, rs, a != nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "judge %s %s %q: %v\n", p.Name(), r.arm, q, err)
			r.judged = false // one failed judgement voids the row: "-", never picked
			continue
		}
		r.relevance += float64(s.Relevance) / float64(len(asks))
		r.et += float64(s.ET) / float64(len(asks))
		r.dated += float64(s.Dated) / float64(len(asks))
	}
	r.medianMS = median(ms)
	return r
}

func main() {
	config := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json with the search keys (myvectorSearch.json beside it holds the quota)")
	names := flag.String("providers", strings.Join(search.DefaultOrder, ","), "comma-separated providers")
	model := flag.String("judge", "claude-haiku-5-5", "Claude judge model")
	news := flag.Bool("news", false, "score the news asks per query arm (N9) instead of the general queries")
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
		if !*news {
			rows = append(rows, bench(p, st, key, *model, queries, nil))
			continue
		}
		for i := range arms {
			if !arms[i].onlySearx || p.Name() == "searxng" {
				rows = append(rows, bench(p, st, key, *model, newsAsks, &arms[i]))
			}
		}
	}
	if *news {
		printNews(rows)
		return
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

// gain is r's mean relevance over the verbatim row of the same provider; N9's rule asks ≥ 0.5.
func gain(rows []row, r row) (float64, bool) {
	for _, v := range rows {
		if v.name == r.name && v.arm == "verbatim" && v.judged && r.judged {
			return r.relevance - v.relevance, true
		}
	}
	return 0, false
}

func printNews(rows []row) {
	fmt.Println("provider | arm | relevance | dated | gain | median ms | errors")
	for _, r := range rows {
		rel, dated, g := "-", "-", "-"
		if r.judged {
			rel, dated = fmt.Sprintf("%.2f", r.relevance), fmt.Sprintf("%.2f", r.dated)
		}
		if d, ok := gain(rows, r); ok && r.arm != "verbatim" {
			g = fmt.Sprintf("%+.2f", d)
			if d >= 0.5 {
				g += " ✓"
			}
		}
		late := ""
		if r.medianMS > search.Timeout.Milliseconds() {
			late = " > timeout"
		}
		fmt.Printf("%s | %s | %s | %s | %s | %d%s | %d\n", r.name, r.arm, rel, dated, g, r.medianMS, late, r.errors)
	}
}
