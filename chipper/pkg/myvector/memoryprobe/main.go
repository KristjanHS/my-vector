// Command memoryprobe runs one memory pass step on a JSONL episode fixture with the configured memory model,
// printing the model reply and the parsed result. From chipper/:
//
//	go run ./pkg/myvector/memoryprobe -fixture pkg/myvector/memory/testdata/session_et.jsonl -step extract|merge|profile|guard|diary
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read memory_model, the Ollama URL and the Claude key from")
	fixture := flag.String("fixture", "", "episodes JSONL (memory.Episode lines)")
	step := flag.String("step", "extract", "extract, merge (extract → merge), profile (→ profile) guard (baseline → merge → profile → check) or diary (one entry per fixture day, no extract)")
	esn := flag.String("esn", "probe", "ESN the fixture's sessions belong to")
	flag.Parse()
	lang.Path = func() string { return *cfgPath }
	if *fixture == "" {
		fail(2, "-fixture is required")
	}
	t, ok := memory.Target(llm.Load(), lang.Estonian, llm.KG{})
	if !ok {
		fail(2, "memory model "+memory.Model()+" gives no target (off, unknown, or claude: with no key)")
	}
	eps, err := memory.ReadJSONL(*fixture)
	if err != nil {
		fail(1, err.Error())
	}
	fmt.Printf("memory model %s %s at %s\n", t.Provider, t.Model, t.BaseURL)
	if *step == "diary" {
		diary(t, eps)
		return
	}
	steps := map[string]int{"extract": 1, "merge": 2, "profile": 3, "guard": 4}[*step]
	if steps == 0 {
		fail(2, "-step "+*step+": not built yet")
	}
	st := &memory.Store{Profiles: map[string]memory.Profile{}}
	st.Facts = extract(t, memory.Sessions(*esn, eps, time.Now()))
	var before map[string]bool
	if steps >= 4 {
		before = guard(t, st, *esn, "baseline")
	}
	if steps >= 2 {
		merge(t, st, *esn)
	}
	if steps >= 3 {
		profile(t, st, *esn)
	}
	if steps >= 4 {
		fmt.Println("-- lost:", memory.Lost(before, guard(t, st, *esn, "check")))
	}
}

func diary(t llm.Target, eps []memory.Episode) {
	byDay := map[string][]memory.Episode{}
	var days []string
	for _, e := range eps {
		d := e.At.Format("2006-01-02")
		if byDay[d] == nil {
			days = append(days, d)
		}
		byDay[d] = append(byDay[d], e)
	}
	for _, d := range days {
		start := time.Now()
		reply, err := memory.Ask(context.Background(), t, memory.DiaryPrompt, memory.DiaryUser(d, byDay[d]), memory.DiaryReplyCap)
		if err != nil {
			fail(1, "diary "+d+": "+err.Error())
		}
		fmt.Printf("\n== diary %s (%s, %d runes, cap %d)\n%s\n", d, time.Since(start).Round(time.Millisecond), len([]rune(reply)), memory.DiaryCap, reply)
	}
}

func guard(t llm.Target, st *memory.Store, esn, phase string) map[string]bool {
	start := time.Now()
	known, err := memory.Answers(context.Background(), t, st, esn)
	if err != nil {
		fail(1, "guard "+phase+": "+err.Error())
	}
	fmt.Printf("\n== guard %s (%s) known: %v\n", phase, time.Since(start).Round(time.Millisecond), known)
	return known
}

func merge(t llm.Target, st *memory.Store, esn string) {
	for _, b := range st.Batches(esn) {
		start := time.Now()
		reply, err := memory.Ask(context.Background(), t, memory.MergePrompt, memory.MergeUser(b), memory.MergeReplyCap)
		if err != nil {
			fail(1, "merge "+b.Category+": "+err.Error())
		}
		fmt.Printf("\n== merge %s (%s)\n-- sent:\n%s-- reply:\n%s\n", b.Category, time.Since(start).Round(time.Millisecond), memory.MergeUser(b), reply)
		ops := memory.ParseMerge(reply, b)
		changed := st.ApplyMerge(b, ops)
		fmt.Printf("-- ops %+v changed=%v\n", ops, changed)
	}
	for _, f := range st.Facts {
		fmt.Printf("%-11s %-9s %s\n", f.Status, f.Category, f.Text)
	}
}

func profile(t llm.Target, st *memory.Store, esn string) {
	start := time.Now()
	text, err := memory.BuildProfile(context.Background(), t, st.ActiveFacts(esn))
	stored := st.SetProfile(esn, text, err, time.Now())
	fmt.Printf("\n== profile (%s, ~%d tokens, cap %d, stored=%v)\n%s\n", time.Since(start).Round(time.Millisecond), prompt.Estimate(text), memory.ProfileCap, stored, text)
}

func extract(t llm.Target, sessions []memory.Session) (all []memory.Fact) {
	for _, s := range sessions {
		start := time.Now()
		reply, err := memory.Ask(context.Background(), t, memory.ExtractPrompt, memory.ExtractUser(s), memory.ExtractReplyCap)
		if err != nil {
			fail(1, s.ID+": "+err.Error())
		}
		fmt.Printf("\n== session %s (%d turns, %s)\n-- reply:\n%s\n", s.ID, len(s.Turns), time.Since(start).Round(time.Millisecond), reply)
		facts, err := memory.ParseExtract(reply, s, time.Now())
		if err != nil {
			fail(1, s.ID+": "+err.Error())
		}
		b, _ := json.MarshalIndent(facts, "", "  ")
		fmt.Printf("-- kept %d:\n%s\n", len(facts), b)
		all = append(all, facts...)
	}
	return all
}

func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, "memoryprobe:", msg)
	os.Exit(code)
}
