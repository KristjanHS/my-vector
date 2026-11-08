// Command memoryprobe runs one memory pass step on a JSONL episode fixture with the configured memory model,
// printing the model reply and the parsed result. From chipper/:
//
//	go run ./pkg/myvector/memoryprobe -fixture pkg/myvector/memory/testdata/session_et.jsonl -step extract
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
)

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read memory_model, the Ollama URL and the Claude key from")
	fixture := flag.String("fixture", "", "episodes JSONL (memory.Episode lines)")
	step := flag.String("step", "extract", "extract (merge, profile, diary, guard land with their stages)")
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
	switch *step {
	case "extract":
		extract(t, memory.Sessions(*esn, eps, time.Now()))
	default:
		fail(2, "-step "+*step+": not built yet")
	}
}

func extract(t llm.Target, sessions []memory.Session) {
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
	}
}

func fail(code int, msg string) {
	fmt.Fprintln(os.Stderr, "memoryprobe:", msg)
	os.Exit(code)
}
