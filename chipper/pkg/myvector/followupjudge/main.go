// Command followupjudge replays logged follow-up transcripts through the listen judge (2026-11-07-follow-up-listening.md (docs/plans/) 7.1).
//
//	journalctl -u wire-pod --since … --until … -o cat | go run ./pkg/myvector/followupjudge -labels ../robot/work/followup-debug/g1-labels.tsv [-show]   # from chipper/
//	journalctl … -o cat | go run ./pkg/myvector/followupjudge -repeat "huvilised tegurid" [-min 0.80]   # 8.1: G2 repeat filter replay, no LLM
//
// Labels: one `skip<TAB>text prefix` or `speak<TAB>text prefix` per line; unmatched transcripts are reported, not scored.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/followup"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// FollowUpLine is arm F's addition to listen.Prompt.
const FollowUpLine = ` A line starting "Vector:" is what you just said yourself; when the newest line answers or replies to it, speak.`

type arm struct{ name, sys string }

var arms = []arm{{"V", listen.Prompt}, {"F", listen.Prompt + FollowUpLine}}

// Pass rule (pre-registered 7.1): SKIP on ≥ skipPct % of the skip lines and SPEAK on every speak line.
const skipPct = 70

// repeatDrops is 8.1's pre-registered bar: drop at least this many marked repeats and no other sentence.
const repeatDrops = 6

type item struct {
	reply, text, want string
	lines             []string // the reply, one logged sentence per line
}

var (
	transcript = regexp.MustCompile(`Transcribed text \([^)]*\): (.*)$`)
	// notReply drops wire-pod's own log lines; what stays between two transcripts is the robot's spoken reply.
	notReply = regexp.MustCompile(`^(myvector|Bot |\(Bot |KG|LLM |Using |Not a custom|Making LLM|No Parameters|EOF$)`)
)

// parse turns `journalctl -o cat` lines into transcripts, each with the reply spoken before it; tail is the last reply.
func parse(r io.Reader) (out []item, tail []string) {
	var said []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if m := transcript.FindStringSubmatch(l); m != nil {
			out = append(out, item{reply: strings.Join(said, " "), text: strings.TrimSpace(m[1]), lines: said})
			said = nil
			continue
		}
		if l != "" && !notReply.MatchString(l) {
			said = append(said, l)
		}
	}
	return out, said
}

// replay runs every reply sentence through followup.Repeats in turn order; history is the last 8 replies as spoken
// (F3's 16 messages), so a dropped question never feeds a later turn. drop[i][j] is reply i's sentence j.
func replay(replies [][]string, min float64) (drop [][]bool) {
	var spoken []string
	for _, r := range replies {
		d := make([]bool, len(r))
		var kept []string
		for j, s := range r {
			if d[j] = followup.Repeats(s, spoken, min); !d[j] {
				kept = append(kept, s)
			}
		}
		drop = append(drop, d)
		if len(kept) > 0 {
			spoken = append(spoken, strings.Join(kept, " "))
		}
		if len(spoken) > 8 {
			spoken = spoken[len(spoken)-8:]
		}
	}
	return drop
}

// repeatPass applies the 8.1 rule.
func repeatPass(markedDropped, otherDropped int) bool {
	return markedDropped >= repeatDrops && otherDropped == 0
}

// runRepeat is -repeat mode: mark is a substring of the repeated question.
func runRepeat(items []item, tail []string, mark string, min float64) {
	var replies [][]string
	for _, it := range items {
		if len(it.lines) > 0 {
			replies = append(replies, it.lines)
		}
	}
	if len(tail) > 0 {
		replies = append(replies, tail)
	}
	var marked, md, od int
	for i, d := range replay(replies, min) {
		for j, x := range d {
			s := replies[i][j]
			m := strings.Contains(strings.ToLower(s), mark)
			if m {
				marked++
			}
			switch {
			case x && m:
				md++
				fmt.Printf("  drop  %q\n", s)
			case x:
				od++
				fmt.Printf("  FALSE drop %q\n", s)
			case m:
				fmt.Printf("  kept  %q\n", s)
			}
		}
	}
	p := repeatPass(md, od)
	fmt.Printf("min %.2f  %d replies  marked %d dropped %d  other dropped %d  pass=%v\n", min, len(replies), marked, md, od, p)
	if !p {
		os.Exit(1)
	}
}

// label sets each item's want from the first matching `want<TAB>prefix` line.
func label(items []item, r io.Reader) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		want, prefix, ok := strings.Cut(sc.Text(), "\t")
		if !ok || (want != "skip" && want != "speak") {
			if strings.TrimSpace(sc.Text()) != "" {
				return fmt.Errorf("label line %q: want skip|speak<TAB>prefix", sc.Text())
			}
			continue
		}
		hit := false
		for i := range items {
			if items[i].want == "" && strings.HasPrefix(items[i].text, prefix) {
				items[i].want, hit = want, true
			}
		}
		if !hit {
			return fmt.Errorf("label %q matches no transcript", prefix)
		}
	}
	return nil
}

// pass applies the 7.1 rule.
func pass(skipOK, nSkip, speakOK, nSpeak int) bool {
	return nSkip > 0 && nSpeak > 0 && skipOK*100 >= skipPct*nSkip && speakOK == nSpeak
}

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read the LLM profiles from")
	labels := flag.String("labels", "", "labels file (private, under robot/work/)")
	show := flag.Bool("show", false, "print every reply, not only the misses")
	mark := flag.String("repeat", "", "8.1 mode: replay the repeat filter; a substring of the repeated question")
	min := flag.Float64("min", followup.RepeatMin, "-repeat: overlap threshold")
	flag.Parse()
	items, tail := parse(os.Stdin)
	if *mark != "" {
		runRepeat(items, tail, strings.ToLower(*mark), *min)
		return
	}
	f, err := os.Open(*labels)
	if err != nil {
		fmt.Fprintln(os.Stderr, "-labels:", err)
		os.Exit(2)
	}
	err = label(items, f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	lang.Path = func() string { return *cfgPath }
	t, ok := llm.ResolveOllama(llm.Load(), lang.Estonian, llm.KG{}, listen.Model)
	if !ok {
		fmt.Fprintln(os.Stderr, "no et Ollama target")
		os.Exit(2)
	}
	c := llm.NewClient(t)
	fmt.Printf("%d transcripts, model %s\n", len(items), t.Model)
	winner := ""
	for _, a := range arms {
		var skipOK, nSkip, speakOK, nSpeak int
		var ms []time.Duration
		for _, it := range items {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			start := time.Now()
			got, err := listen.AskWith(ctx, c, t.Model, a.sys, listen.User([]string{"Vector: " + it.reply}, it.text))
			cancel()
			ms = append(ms, time.Since(start))
			sp := err == nil && listen.Parse(got)
			verdict := map[bool]string{false: "skip", true: "speak"}[sp]
			hit := err == nil && verdict == it.want
			switch it.want {
			case "skip":
				nSkip++
				if hit {
					skipOK++
				}
			case "speak":
				nSpeak++
				if hit {
					speakOK++
				}
			}
			tag := map[string]string{"": "  - ", "skip": "miss", "speak": "miss"}[it.want]
			if hit {
				tag = "  ok"
			}
			if !hit || *show {
				fmt.Printf("  %s %s want=%-5s %-60q → %q %v\n", a.name, tag, it.want, it.text, got, err)
			}
		}
		med := median(ms)
		p := pass(skipOK, nSkip, speakOK, nSpeak)
		fmt.Printf("arm %s  skip %d/%d  speak %d/%d  median %d ms  pass=%v\n", a.name, skipOK, nSkip, speakOK, nSpeak, med.Milliseconds(), p)
		if p && winner == "" {
			winner = a.name
		}
	}
	if winner == "" {
		fmt.Println("winner: none")
		os.Exit(1)
	}
	fmt.Println("winner:", winner)
}

func median(ms []time.Duration) time.Duration {
	if len(ms) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ms...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}
