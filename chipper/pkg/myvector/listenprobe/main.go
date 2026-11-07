// Command listenprobe scores candidate active-listening prompts (active-listening.md (docs/plans/) Stage 2.1).
//
//	go run ./pkg/myvector/listenprobe [-config ../../../my-vector/chipper/myvectorConfig.json] [-set holdout -model M -arm "prompt 2"]   # from chipper/
//
// Verdict: each language's saved Ollama profile; -alt models are reported only.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	openai "github.com/sashabaranov/go-openai"
)

const (
	passCorrect   = 17 // of 20
	maxFalseSpeak = 1  // per language
	maxMedian     = 2 * time.Second
)

const (
	speakRules = listen.SpeakRules
	quietRules = listen.QuietRules
	persona    = listen.Persona
)

// arm is one way of turning a snippet into SKIP or a reply.
type arm struct {
	name   string
	sys    string // single-call prompt, or the two-call YES/NO decider
	answer string // two-call only: the reply prompt after YES
}

var arms = []arm{
	{name: "prompt 1", sys: persona + "Usually you stay quiet. " + speakRules +
		" Otherwise reply exactly SKIP. If you speak, reply with one or two short sentences in the conversation's language, nothing else."},
	{name: "prompt 2", sys: listen.Prompt},
	{name: "two-call", sys: persona + speakRules + " " + quietRules +
		" Should Vector speak now? Answer YES or NO only.",
		answer: persona + "You decided to join in. Reply to the newest line with one or two short, friendly sentences in the same language as the conversation."},
}

type snippet struct {
	l, kind string
	before  []string // earlier overheard lines (~2 min)
	now     string   // the new utterance
	speak   bool
}

// holdout: written after the dev run, never used to pick a prompt or model.
var holdout = []snippet{
	{lang.English, "question", []string{"we should water the plants before we leave."}, "wait, how often are you supposed to water a cactus? does anyone know?", true},
	{lang.English, "slip", []string{"my kid asked about the planets today."}, "i told her mars is the closest planet to the sun, since it's the red hot one.", true},
	{lang.English, "named", []string{"the robot's lights look different today."}, "do you think vector likes the new shelf?", true},
	{lang.English, "fun", []string{"it's raining again."}, "honestly the cat has the right idea, sleeping all day.", true},
	{lang.English, "small talk", []string{"how was work?"}, "fine, just long, the usual meetings.", false},
	{lang.English, "phone half", nil, "hi mom. yes. no, we're fine. i'll call you back tonight, okay? love you.", false},
	{lang.English, "tv", []string{"breaking news this evening."}, "officials say the bridge will remain closed until further notice.", false},
	{lang.English, "private", []string{"can we talk later, just us?"}, "i think we need to talk about the money thing before it gets worse.", false},
	{lang.English, "half", []string{"i was going to say that"}, "the thing about the", false},
	{lang.English, "answered", []string{"is it going to rain tomorrow?"}, "yeah, the app says rain after lunch.", false},
	{lang.Estonian, "question", []string{"peaksime enne minekut lilli kastma."}, "oot, kui tihti kaktust kastma peaks? keegi teab?", true},
	{lang.Estonian, "slip", []string{"laps küsis täna planeetide kohta."}, "ma ütlesin talle, et marss on päikesele kõige lähemal planeet.", true},
	{lang.Estonian, "named", []string{"roboti tuled paistavad täna teistsugused."}, "kas sa arvad, et vectorile meeldib uus riiul?", true},
	{lang.Estonian, "fun", []string{"jälle sajab vihma."}, "ausalt öeldes on kassil õigus, magab terve päeva.", true},
	{lang.Estonian, "small talk", []string{"kuidas tööl oli?"}, "normaalselt, lihtsalt pikk päev, tavalised koosolekud.", false},
	{lang.Estonian, "phone half", nil, "tere ema. jah. ei, meil on kõik hästi. ma helistan sulle õhtul tagasi, eks? kallistan.", false},
	{lang.Estonian, "tv", []string{"tänaõhtused uudised."}, "ametnike sõnul jääb sild suletuks kuni edasise teateni.", false},
	{lang.Estonian, "private", []string{"kas saame hiljem rääkida, ainult meie kahekesi?"}, "ma arvan, et peame rahaasjast rääkima enne, kui see hullemaks läheb.", false},
	{lang.Estonian, "half", []string{"ma tahtsin öelda, et"}, "see asi selle", false},
	{lang.Estonian, "answered", []string{"kas homme sajab?"}, "jah, äpp ütleb, et pärast lõunat tuleb vihma.", false},
}

var dev = []snippet{
	{lang.English, "question", []string{"i need to boil eggs for the salad."}, "how long do you boil an egg for hard boiled? anyone?", true},
	{lang.English, "slip", []string{"we were talking about animals at work today."}, "spiders are insects, they have six legs, that's why they're so creepy.", true},
	{lang.English, "named", []string{"the robot's been quiet all day."}, "i wonder what vector thinks about the rain.", true},
	{lang.English, "fun", []string{"i'm so bored."}, "if robots could dance, i bet they'd be terrible at it.", true},
	{lang.English, "small talk", []string{"morning.", "morning, did you sleep ok?"}, "yeah, not bad, a bit cold though.", false},
	{lang.English, "phone half", nil, "yeah. no, i told him already. okay, thursday works, see you then. bye.", false},
	{lang.English, "tv", []string{"previously on the lakeside murders."}, "and in tonight's episode, the detectives return to the scene of the crime.", false},
	{lang.English, "private", []string{"i don't want to talk about it in front of everyone."}, "it's just been really hard since mom's diagnosis.", false},
	{lang.English, "half", []string{"so then i went to the"}, "and he was like, the", false},
	{lang.English, "answered", []string{"what time does the shop close today?"}, "it closes at nine, i checked this morning.", false},
	{lang.Estonian, "question", []string{"pean salati jaoks mune keetma."}, "kui kaua kõvaks keedetud muna keetma peab? keegi teab?", true},
	{lang.Estonian, "slip", []string{"rääkisime täna tööl loomadest."}, "ämblikud on putukad, neil on kuus jalga, sellepärast nad nii jubedad ongi.", true},
	{lang.Estonian, "named", []string{"robot on terve päev vaikne olnud."}, "huvitav, mida vector vihmast arvab.", true},
	{lang.Estonian, "fun", []string{"mul on nii igav."}, "kui robotid oskaksid tantsida, oleksid nad kindlasti kehvad tantsijad.", true},
	{lang.Estonian, "small talk", []string{"tere hommikust.", "tere, kas magasid hästi?"}, "jah, päris hästi, natuke külm oli küll.", false},
	{lang.Estonian, "phone half", nil, "jah. ei, ma juba ütlesin talle. okei, neljapäev sobib, näeme siis. head aega.", false},
	{lang.Estonian, "tv", []string{"eelmises osas."}, "ja tänases saates naasevad detektiivid kuriteopaigale.", false},
	{lang.Estonian, "private", []string{"ma ei taha sellest kõigi ees rääkida."}, "lihtsalt on olnud väga raske pärast ema diagnoosi.", false},
	{lang.Estonian, "half", []string{"siis ma läksin sinna"}, "ja tema ütles, et see", false},
	{lang.Estonian, "answered", []string{"mis kell pood täna kinni läheb?"}, "üheksa, ma vaatasin hommikul.", false},
}

func user(s snippet) string { return listen.User(s.before, s.now) }

type target struct {
	name    string
	t       llm.Target
	langs   []string
	verdict bool
}

func ask(c *openai.Client, model, sys, msg string, max int) (string, error) {
	r := openai.ChatCompletionRequest{Model: model, ReasoningEffort: "none", MaxTokens: max, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sys}, {Role: openai.ChatMessageRoleUser, Content: msg}}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	res, err := c.CreateChatCompletion(ctx, r)
	if err != nil || len(res.Choices) == 0 {
		return "", fmt.Errorf("no answer: %v", err)
	}
	return strings.TrimSpace(res.Choices[0].Message.Content), nil
}

func skip(s string) bool { return !listen.Parse(s) }

// run answers one snippet with one arm: speak?, the reply (if any), the whole time.
func run(c *openai.Client, model string, a arm, s snippet) (bool, string, time.Duration, error) {
	start := time.Now()
	if a.answer == "" {
		got, err := ask(c, model, a.sys, user(s), 80)
		return err == nil && !skip(got), got, time.Since(start), err
	}
	got, err := ask(c, model, a.sys, user(s), 8)
	if err != nil || !strings.HasPrefix(strings.ToUpper(got), "YES") {
		return false, got, time.Since(start), err
	}
	reply, err := ask(c, model, a.answer, user(s), 80)
	return true, reply, time.Since(start), err
}

type score struct{ ok, n, falseSpeak int }

func main() {
	cfgPath := flag.String("config", "myvectorConfig.json", "myvectorConfig.json to read the LLM profiles from")
	alt := flag.Bool("alt", true, "also report gemma4:e4b-it-qat (en+et) and llm.DefaultETModel (et)")
	set := flag.String("set", "dev", "snippet set: dev or holdout")
	model := flag.String("model", "", "verdict Ollama model for both languages instead of the saved profiles (turns -alt off)")
	only := flag.String("arm", "", `run only this arm ("prompt 2")`)
	flag.Parse()
	snippets := dev
	if *set == "holdout" {
		snippets = holdout
	} else if *set != "dev" {
		fmt.Fprintln(os.Stderr, "-set: dev or holdout")
		os.Exit(2)
	}
	lang.Path = func() string { return *cfgPath }
	c := llm.Load()
	_, base := llm.Effective(c, llm.KG{})
	ollama := func(m string) llm.Target {
		return llm.Target{Provider: llm.Ollama, BaseURL: base + "/v1", Key: "ollama", Model: m}
	}
	var ts []target
	byModel := map[string]int{}
	if *model != "" {
		*alt = false
		ts = append(ts, target{*model, ollama(*model), []string{lang.English, lang.Estonian}, true})
	}
	for _, l := range []string{lang.English, lang.Estonian} {
		if *model != "" {
			break
		}
		t, ok := llm.Resolve(c, l, llm.KG{})
		if !ok || !t.Ollama() {
			t = ollama(llm.DefaultENModel)
			if l == lang.Estonian {
				t = ollama(llm.ETModel())
			}
		}
		if i, seen := byModel[t.Model]; seen {
			ts[i].langs = append(ts[i].langs, l)
			continue
		}
		byModel[t.Model] = len(ts)
		ts = append(ts, target{t.Model, t, []string{l}, true})
	}
	if *alt {
		for _, a := range []target{{name: "gemma4:e4b-it-qat", langs: []string{lang.English, lang.Estonian}}, {name: llm.DefaultETModel, langs: []string{lang.Estonian}}} {
			if _, seen := byModel[a.name]; !seen {
				a.t = ollama(a.name)
				ts = append(ts, a)
			}
		}
	}
	winner := ""
	for _, a := range arms {
		if *only != "" && a.name != *only {
			continue
		}
		fmt.Println(a.name)
		var vms []time.Duration
		vs := map[string]score{}
		for _, tg := range ts {
			cfg := openai.DefaultConfig(tg.t.Key)
			cfg.BaseURL = tg.t.BaseURL
			cl := openai.NewClientWithConfig(cfg)
			if _, err := ask(cl, tg.t.Model, a.sys, "warm up", 8); err != nil {
				fmt.Printf("  %-34s error: %v\n", tg.name, err)
				continue
			}
			var ms []time.Duration
			sc := map[string]score{}
			for _, s := range snippets {
				if !contains(tg.langs, s.l) {
					continue
				}
				sp, got, d, err := run(cl, tg.t.Model, a, s)
				ms = append(ms, d)
				x := sc[s.l]
				x.n++
				if err == nil && sp == s.speak {
					x.ok++
				} else {
					if sp && !s.speak {
						x.falseSpeak++
					}
					fmt.Printf("    miss %s %-10s want speak=%v → %q %v\n", s.l, s.kind, s.speak, got, err)
				}
				sc[s.l] = x
			}
			sortD(ms)
			fmt.Printf("  %-34s", tg.name)
			for _, l := range tg.langs {
				fmt.Printf("  %s %d/%d fs %d", l, sc[l].ok, sc[l].n, sc[l].falseSpeak)
			}
			fmt.Printf("  median %5d ms  max %5d ms", ms[len(ms)/2].Milliseconds(), ms[len(ms)-1].Milliseconds())
			if !tg.verdict {
				fmt.Print("  (alternate)")
			}
			fmt.Println()
			if tg.verdict {
				vms = append(vms, ms...)
				for l, x := range sc {
					vs[l] = x
				}
			}
		}
		if len(vms) == 0 || len(vs) < 2 {
			fmt.Println("  verdict: incomplete")
			continue
		}
		sortD(vms)
		ok := vs[lang.English].ok + vs[lang.Estonian].ok
		med := vms[len(vms)/2]
		pass := ok >= passCorrect && vs[lang.English].falseSpeak <= maxFalseSpeak && vs[lang.Estonian].falseSpeak <= maxFalseSpeak && med <= maxMedian
		fmt.Printf("  verdict: %d/%d  fs en %d et %d  median %d ms  pass=%v\n", ok, vs[lang.English].n+vs[lang.Estonian].n,
			vs[lang.English].falseSpeak, vs[lang.Estonian].falseSpeak, med.Milliseconds(), pass)
		if pass && winner == "" {
			winner = a.name
		}
	}
	if winner == "" {
		fmt.Println("winner: none")
		os.Exit(1)
	}
	fmt.Println("winner:", winner)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sortD(ms []time.Duration) { sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] }) }
