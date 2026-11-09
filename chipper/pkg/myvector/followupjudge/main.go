// Command followupjudge replays logged follow-up transcripts through the listen judge (2026-11-07-follow-up-listening.md (docs/plans/) 7.1).
//
//	journalctl -u wire-pod --since … --until … -o cat | go run ./pkg/myvector/followupjudge -labels ../robot/work/followup-debug/g1-labels.tsv [-show]   # from chipper/
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

type item struct{ reply, text, want string }

var (
	transcript = regexp.MustCompile(`Transcribed text \([^)]*\): (.*)$`)
	// notReply drops wire-pod's own log lines; what stays between two transcripts is the robot's spoken reply.
	notReply = regexp.MustCompile(`^(myvector|Bot |\(Bot |KG|LLM |Using |Not a custom|Making LLM|No Parameters|EOF$)`)
)

// parse turns `journalctl -o cat` lines into transcripts, each with the reply spoken before it.
func parse(r io.Reader) []item {
	var out []item
	var said []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if m := transcript.FindStringSubmatch(l); m != nil {
			out = append(out, item{reply: strings.Join(said, " "), text: strings.TrimSpace(m[1])})
			said = nil
			continue
		}
		if l != "" && !notReply.MatchString(l) {
			said = append(said, l)
		}
	}
	return out
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
	flag.Parse()
	items := parse(os.Stdin)
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
