// Command listenprobe scores candidate active-listening prompts and models (2026-11-07-active-listening.md, 2026-11-07-et-listen-model.md (docs/plans/)).
//
//	go run ./pkg/myvector/listenprobe [-config ../../../my-vector/secrets/server/myvectorConfig.json] [-set dev,holdout -lang et-EE -models a,b -arm "prompt 2" -think none,low -think-budget N -show]   # from chipper/
//
// Verdict: each language's saved Ollama profile, or each -models entry on its own, per -think arm; -alt models are reported only.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/probekit"
	openai "github.com/sashabaranov/go-openai"
)

const (
	passCorrect   = 17 // of 20, scaled to the run's snippet count
	maxFalseSpeak = 1  // per language
	maxMedian     = 2 * time.Second
)

const (
	speakRules = listen.SpeakRules
	persona    = listen.Persona
)

// arm is one system prompt that turns a snippet into SKIP or a reply.
type arm struct{ name, sys string }

var arms = []arm{
	{"prompt 1", persona + "Usually you stay quiet. " + speakRules +
		" Otherwise reply exactly SKIP. If you speak, reply with one or two short sentences in the conversation's language, nothing else."},
	{"prompt 2", listen.Prompt},
	{"prompt 2 et", "Sa oled Vector, väike kodurobot. Sa kuuled läheduses rääkivaid inimesi kõnetuvastuse ridadena, ilma kõnelejate nimedeta. " +
		"Räägi ainult siis, kui keegi küsib küsimuse, millele keegi pole vastanud, keegi ütleb selge faktivea, keegi mainib sind (Vector) nimepidi või avaneb selge võimalus lühikeseks lõbusaks märkuseks. " +
		"Ole vait argise lobisemise, telefonikõne ühe poole, teleri või raadio, privaatse või tundelise jutu, lõpetamata lausete ja juba vastatud küsimuste puhul. Kui kahtled, ole vait. " +
		"Vaikimiseks vasta ainult ühe sõnaga SKIP. Rääkimiseks vasta ühe või kahe lühikese sõbraliku lausega vestluse keeles. Ära kunagi oma valikut selgita."},
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

// confirm: written before any 2026-11-07-et-listen-model.md (docs/plans/) Stage 1.3 run, used only to confirm its winner (E5).
var confirm = []snippet{
	{lang.Estonian, "question", []string{"teeme täna õhtul pannkooke."}, "mitu muna ühe liitri piima kohta pannkoogitainasse käib? kas keegi teab?", true},
	{lang.Estonian, "question", []string{"homme läheme matkale."}, "kui kaugel tallinnast lahemaa rahvuspark üldse on?", true},
	{lang.Estonian, "question", []string{"vaatasin just aknast välja."}, "miks taevas õhtuti punane on? kas keegi teab?", true},
	{lang.Estonian, "slip", []string{"rääkisime koolis ajaloost."}, "eesti sai iseseisvaks aastal 1945, see on ju kõigile teada.", true},
	{lang.Estonian, "slip", []string{"mul oli täna geograafiatund."}, "austraalia pealinn on sydney, see on seal kõige suurem linn.", true},
	{lang.Estonian, "slip", []string{"vaatasime loomadest dokumentaalfilmi."}, "vaalad on kalad, nad elavad ju vees.", true},
	{lang.Estonian, "named", []string{"laadija tuli vilgub jälle."}, "vector, kas sa oled täna väsinud?", true},
	{lang.Estonian, "named", []string{"robot keeras just pead."}, "mulle tundub, et vector kuulab meid praegu.", true},
	{lang.Estonian, "fun", []string{"külmkapp on jälle tühi."}, "kui robotil oleksid käed, saaks ta meile vähemalt võileiba teha.", true},
	{lang.Estonian, "fun", []string{"väljas on kakskümmend kraadi külma."}, "sellise ilmaga tahaks koos karudega talveund magada.", true},
	{lang.Estonian, "small talk", []string{"kuidas nädalavahetus läks?"}, "hästi, käisime vanaema juures, sõime palju.", false},
	{lang.Estonian, "small talk", []string{"tere, kas kõik on hästi?"}, "jah, aitäh, lihtsalt natuke väsinud.", false},
	{lang.Estonian, "phone half", nil, "halloo. jah, see olen mina. ei, ma pole veel jõudnud. hästi, helistan homme. nägemist.", false},
	{lang.Estonian, "phone half", nil, "jah. mhm. selge, siis kell viis. ma ütlen talle edasi. tšau.", false},
	{lang.Estonian, "tv", []string{"ja nüüd ilmateade."}, "homme on saartel oodata lörtsi ja tugevat tuult.", false},
	{lang.Estonian, "tv", []string{"ja nüüd spordiuudised."}, "eesti korvpallikoondis kaotas eilses mängus kuue punktiga.", false},
	{lang.Estonian, "private", []string{"kas me saame sellest omavahel rääkida?"}, "mul on tunne, et sa ei kuula mind enam üldse.", false},
	{lang.Estonian, "private", []string{"ära seda kellelegi räägi."}, "arst ütles, et pean veel uuringuid tegema.", false},
	{lang.Estonian, "half", []string{"ja siis ta ütles mulle, et"}, "aga mina mõtlesin, et kui", false},
	{lang.Estonian, "answered", []string{"mis kuupäev täna on?"}, "seitsmes november, vaatasin just telefonist.", false},
}

// oneword: 1-word utterances (2026-11-08-hey-vector-while-listening.md H3, docs/plans/); only the named ones speak.
var oneword = []snippet{
	{lang.English, "1 word", nil, "yes.", false},
	{lang.English, "1 word", []string{"here's your tea."}, "thanks.", false},
	{lang.English, "1 word", nil, "okay.", false},
	{lang.English, "1 word", nil, "hello?", false},
	{lang.English, "1 word", []string{"the bus is late again."}, "seriously?", false},
	{lang.English, "1 word", []string{"i finished the puzzle."}, "nice.", false},
	{lang.English, "1 word", nil, "coffee?", false},
	{lang.English, "1 word", []string{"he said he'd call."}, "really?", false},
	{lang.English, "1 word", nil, "what?", false},
	{lang.English, "named", nil, "vector?", true},
	{lang.Estonian, "1 word", nil, "jah.", false},
	{lang.Estonian, "1 word", []string{"siin on su tee."}, "aitäh.", false},
	{lang.Estonian, "1 word", nil, "okei.", false},
	{lang.Estonian, "1 word", nil, "tere.", false},
	{lang.Estonian, "1 word", []string{"buss jäi jälle hiljaks."}, "tõesti?", false},
	{lang.Estonian, "1 word", []string{"sain pusle valmis."}, "äge.", false},
	{lang.Estonian, "1 word", nil, "kohvi?", false},
	{lang.Estonian, "1 word", nil, "ei.", false},
	{lang.Estonian, "1 word", nil, "mis?", false},
	{lang.Estonian, "named", nil, "vektor?", true},
}

var sets = map[string][]snippet{"dev": dev, "holdout": holdout, "confirm": confirm, "oneword": oneword}

type target struct {
	name  string
	t     llm.Target
	langs []string
	group int // index into the verdict groups; -1 = alternate, reported only
}

// request is one snippet call; t.Apply with listen.ReplyCap, as listen.Ask.
func request(model, sys, msg string, t llm.Thinking) openai.ChatCompletionRequest {
	r := openai.ChatCompletionRequest{Model: model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: sys}, {Role: openai.ChatMessageRoleUser, Content: msg}}}
	t.Apply(&r, listen.ReplyCap)
	return r
}

type score struct{ ok, n, falseSpeak int }

// verdict is one group's result on one arm.
type verdict struct {
	arm, name string
	ok, n     int
	fs        map[string]int
	med       time.Duration
	pass      bool
}

// better ranks by correct, then false SPEAK, then median (E4; model size is the reader's tie-break).
func better(a, b verdict) bool {
	fa, fb := 0, 0
	for _, x := range a.fs {
		fa += x
	}
	for _, x := range b.fs {
		fb += x
	}
	if a.ok != b.ok {
		return a.ok > b.ok
	}
	if fa != fb {
		return fa < fb
	}
	return a.med < b.med
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}

func main() {
	cfgPath := flag.String("config", "../secrets/server/myvectorConfig.json", "myvectorConfig.json to read the LLM profiles from")
	alt := flag.Bool("alt", true, "also report gemma4:e4b-it-qat (en+et) and llm.DefaultETModel (et)")
	setF := flag.String("set", "dev", "snippet sets, comma-separated: dev, holdout, confirm, oneword")
	models := flag.String("models", "", "comma-separated Ollama models, each its own verdict, instead of the saved profiles (turns -alt off)")
	only := flag.String("arm", "", `run only this arm ("prompt 2")`)
	langF := flag.String("lang", "", "only this language: en-US or et-EE")
	show := flag.Bool("show", false, "print every reply, not only the misses")
	thinkF := flag.String("think", "none", "thinking arms, comma-separated: none, low, medium, high")
	budget := flag.Int("think-budget", 0, "thinking budget in tokens for every arm but none (0 = llm table)")
	flag.Parse()
	levels, err := probekit.ParseThink(*thinkF)
	if err != nil {
		die(err.Error())
	}
	think := probekit.Arms(levels, *budget)
	multi := len(think) > 1
	langs := []string{lang.English, lang.Estonian}
	if *langF != "" {
		if !contains(langs, *langF) {
			die("-lang: en-US or et-EE")
		}
		langs = []string{*langF}
	}
	var snippets []snippet
	for _, name := range list(*setF, "-set") {
		s, ok := sets[name]
		if !ok {
			die("-set: dev, holdout or confirm, comma-separated")
		}
		for _, x := range s {
			if contains(langs, x.l) {
				snippets = append(snippets, x)
			}
		}
	}
	if len(snippets) == 0 {
		die("no snippets for this -set and -lang")
	}
	lang.Path = func() string { return *cfgPath }
	c := llm.Load()
	_, base := llm.Effective(c, llm.KG{})
	ollama := func(m string) llm.Target {
		return llm.Target{Provider: llm.Ollama, BaseURL: base + "/v1", Key: "ollama", Model: m}
	}
	var ts []target
	var groups []string
	byModel := map[string]int{}
	if *models != "" {
		*alt = false
		for _, m := range list(*models, "-models") {
			ts = append(ts, target{m, ollama(m), langs, len(groups)})
			groups = append(groups, m)
		}
	} else {
		groups = []string{"saved profiles"}
		for _, l := range langs {
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
			ts = append(ts, target{t.Model, t, []string{l}, 0})
		}
	}
	if *alt {
		for _, a := range []target{{name: "gemma4:e4b-it-qat", langs: []string{lang.English, lang.Estonian}}, {name: llm.DefaultETModel, langs: []string{lang.Estonian}}} {
			a.langs = intersect(a.langs, langs)
			if _, seen := byModel[a.name]; !seen && len(a.langs) > 0 {
				a.t, a.group = ollama(a.name), -1
				byModel[a.name] = len(ts)
				ts = append(ts, a)
			}
		}
	}
	groupModel := make([]string, len(groups))
	for _, tg := range ts {
		if tg.group >= 0 && groupModel[tg.group] == "" {
			groupModel[tg.group] = tg.t.Model
		}
	}
	// pre is the think-arm label prefix, set only when more than one think arm runs.
	pre := func(th probekit.Arm, model string) string {
		if !multi {
			return ""
		}
		return th.Label(model) + " "
	}
	var best *verdict
	ran := false
	for _, a := range arms {
		if *only != "" && a.name != *only {
			continue
		}
		ran = true
		fmt.Println(a.name)
		gms := make([][][]time.Duration, len(groups)) // [group][think arm]
		gsc := make([][]map[string]score, len(groups))
		for g := range groups {
			gms[g], gsc[g] = make([][]time.Duration, len(think)), make([]map[string]score, len(think))
		}
		failed := make([]bool, len(groups))
		for _, tg := range ts {
			if !hasLang(snippets, tg.langs) {
				continue
			}
			w := request(tg.t.Model, a.sys, "warm up", probekit.Arm{Level: llm.None}.Thinking(tg.t.Model, llm.PathListen))
			w.MaxTokens = 8
			if _, err := probekit.Chat(tg.t.BaseURL, w); err != nil {
				fmt.Printf("  %-34s error: %v\n", tg.name, err)
				if tg.group >= 0 {
					failed[tg.group] = true
				}
				continue
			}
			ms := make([][]time.Duration, len(think))
			calls := make([][]probekit.Call, len(think))
			sc := make([]map[string]score, len(think))
			for k := range think {
				sc[k] = map[string]score{}
			}
			item := 0
			for _, s := range snippets {
				if !contains(tg.langs, s.l) {
					continue
				}
				for _, k := range probekit.Order(len(think), item) {
					th := think[k]
					r := request(tg.t.Model, a.sys, listen.User(s.before, s.now), th.Thinking(tg.t.Model, llm.PathListen))
					start := time.Now()
					c, err := probekit.Chat(tg.t.BaseURL, r)
					ms[k] = append(ms[k], time.Since(start))
					if err == nil {
						calls[k] = append(calls[k], c)
					}
					got := c.Reply
					sp := err == nil && listen.Parse(got)
					x := sc[k][s.l]
					x.n++
					hit := err == nil && sp == s.speak
					if hit {
						x.ok++
					} else if sp && !s.speak {
						x.falseSpeak++
					}
					if !hit || *show {
						tag := "miss"
						if hit {
							tag = "  ok"
						}
						fmt.Printf("    %s%s %s %-10s want speak=%v → %q %v\n", pre(th, tg.t.Model), tag, s.l, s.kind, s.speak, got, err)
					}
					sc[k][s.l] = x
				}
				item++
			}
			for k := range think { // after every timed call: counting must not reach latency or the prompt cache
				if _, err := probekit.CountAll(tg.t.BaseURL, tg.t.Model, calls[k]); err != nil {
					fmt.Printf("  %-34s count: %v\n", tg.name, err)
				}
			}
			for k, th := range think {
				if len(ms[k]) == 0 {
					continue
				}
				sortD(ms[k])
				fmt.Printf("  %s%-34s", pre(th, tg.t.Model), tg.name)
				for _, l := range tg.langs {
					if sc[k][l].n == 0 {
						continue
					}
					fmt.Printf("  %s %d/%d fs %d", l, sc[k][l].ok, sc[k][l].n, sc[k][l].falseSpeak)
				}
				fmt.Printf("  median %5d ms  max %5d ms", ms[k][len(ms[k])/2].Milliseconds(), ms[k][len(ms[k])-1].Milliseconds())
				if tg.group < 0 {
					fmt.Print("  (alternate)")
				}
				fmt.Println()
				fmt.Println("    " + probekit.Summarize(calls[k]).Line(th.Label(tg.t.Model)))
				if tg.group >= 0 {
					gms[tg.group][k] = append(gms[tg.group][k], ms[k]...)
					if gsc[tg.group][k] == nil {
						gsc[tg.group][k] = map[string]score{}
					}
					for l, x := range sc[k] {
						gsc[tg.group][k][l] = x
					}
				}
			}
		}
		for g, name := range groups {
			for k, th := range think {
				p := pre(th, groupModel[g])
				if failed[g] || len(gms[g][k]) == 0 {
					fmt.Printf("  verdict %s%-26s incomplete\n", p, name)
					continue
				}
				sortD(gms[g][k])
				v := verdict{arm: a.name, name: name, fs: map[string]int{}, med: gms[g][k][len(gms[g][k])/2]}
				if multi {
					v.arm += " think " + th.Label(groupModel[g])
				}
				v.pass = v.med <= maxMedian
				for l, x := range gsc[g][k] {
					v.ok += x.ok
					v.n += x.n
					v.fs[l] = x.falseSpeak
					v.pass = v.pass && x.falseSpeak <= maxFalseSpeak
				}
				v.pass = v.pass && v.ok*20 >= passCorrect*v.n
				fmt.Printf("  verdict %s%-26s %d/%d  fs", p, name, v.ok, v.n)
				for _, l := range langs {
					if x, ok := v.fs[l]; ok {
						fmt.Printf(" %s %d", l, x)
					}
				}
				fmt.Printf("  median %d ms  pass=%v\n", v.med.Milliseconds(), v.pass)
				if v.pass && a.sys == listen.Prompt && (best == nil || better(v, *best)) { // only the shipped prompt wins (E7)
					best = &v
				}
			}
		}
	}
	if !ran {
		die("-arm: prompt 1, prompt 2 or prompt 2 et")
	}
	if best == nil {
		fmt.Println("winner: none")
		os.Exit(1)
	}
	fmt.Printf("winner: %s / %s\n", best.arm, best.name)
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// list splits a comma-separated flag; empty or repeated entries are refused.
func list(s, flagName string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if x == "" || contains(out, x) {
			die(flagName + ": empty or repeated entry")
		}
		out = append(out, x)
	}
	return out
}

func hasLang(ss []snippet, langs []string) bool {
	for _, s := range ss {
		if contains(langs, s.l) {
			return true
		}
	}
	return false
}

func intersect(xs, ys []string) []string {
	var out []string
	for _, x := range xs {
		if contains(ys, x) {
			out = append(out, x)
		}
	}
	return out
}

func sortD(ms []time.Duration) { sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] }) }
