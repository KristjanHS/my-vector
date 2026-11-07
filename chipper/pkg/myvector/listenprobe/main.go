// Command listenprobe scores candidate active-listening prompts and models (active-listening.md, et-listen-model.md (docs/plans/)).
//
//	go run ./pkg/myvector/listenprobe [-config ../../../my-vector/chipper/myvectorConfig.json] [-set dev,holdout -lang et-EE -models a,b -arm "prompt 2" -show]   # from chipper/
//
// Verdict: each language's saved Ollama profile, or each -models entry on its own; -alt models are reported only.
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

// confirm: written before any et-listen-model.md Stage 1.3 run, used only to confirm its winner (E5).
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

var sets = map[string][]snippet{"dev": dev, "holdout": holdout, "confirm": confirm}

type target struct {
	name  string
	t     llm.Target
	langs []string
	group int // index into the verdict groups; -1 = alternate, reported only
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
	cfgPath := flag.String("config", "myvectorConfig.json", "myvectorConfig.json to read the LLM profiles from")
	alt := flag.Bool("alt", true, "also report gemma4:e4b-it-qat (en+et) and llm.DefaultETModel (et)")
	setF := flag.String("set", "dev", "snippet sets, comma-separated: dev, holdout, confirm")
	models := flag.String("models", "", "comma-separated Ollama models, each its own verdict, instead of the saved profiles (turns -alt off)")
	only := flag.String("arm", "", `run only this arm ("prompt 2")`)
	langF := flag.String("lang", "", "only this language: en-US or et-EE")
	show := flag.Bool("show", false, "print every reply, not only the misses")
	flag.Parse()
	langs := []string{lang.English, lang.Estonian}
	if *langF != "" {
		if !contains(langs, *langF) {
			die("-lang: en-US or et-EE")
		}
		langs = []string{*langF}
	}
	var snippets []snippet
	for _, name := range strings.Split(*setF, ",") {
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
		for _, m := range strings.Split(*models, ",") {
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
				ts = append(ts, a)
			}
		}
	}
	var best *verdict
	ran := false
	for _, a := range arms {
		if *only != "" && a.name != *only {
			continue
		}
		ran = true
		fmt.Println(a.name)
		gms := make([][]time.Duration, len(groups))
		gsc := make([]map[string]score, len(groups))
		failed := make([]bool, len(groups))
		for _, tg := range ts {
			cfg := openai.DefaultConfig(tg.t.Key)
			cfg.BaseURL = tg.t.BaseURL
			cl := openai.NewClientWithConfig(cfg)
			if _, err := ask(cl, tg.t.Model, a.sys, "warm up", 8); err != nil {
				fmt.Printf("  %-34s error: %v\n", tg.name, err)
				if tg.group >= 0 {
					failed[tg.group] = true
				}
				continue
			}
			var ms []time.Duration
			sc := map[string]score{}
			for _, s := range snippets {
				if !contains(tg.langs, s.l) {
					continue
				}
				start := time.Now()
				got, err := ask(cl, tg.t.Model, a.sys, listen.User(s.before, s.now), 80)
				ms = append(ms, time.Since(start))
				sp := err == nil && listen.Parse(got)
				x := sc[s.l]
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
					fmt.Printf("    %s %s %-10s want speak=%v → %q %v\n", tag, s.l, s.kind, s.speak, got, err)
				}
				sc[s.l] = x
			}
			if len(ms) == 0 {
				continue
			}
			sortD(ms)
			fmt.Printf("  %-34s", tg.name)
			for _, l := range tg.langs {
				fmt.Printf("  %s %d/%d fs %d", l, sc[l].ok, sc[l].n, sc[l].falseSpeak)
			}
			fmt.Printf("  median %5d ms  max %5d ms", ms[len(ms)/2].Milliseconds(), ms[len(ms)-1].Milliseconds())
			if tg.group < 0 {
				fmt.Print("  (alternate)")
			}
			fmt.Println()
			if tg.group >= 0 {
				gms[tg.group] = append(gms[tg.group], ms...)
				if gsc[tg.group] == nil {
					gsc[tg.group] = map[string]score{}
				}
				for l, x := range sc {
					gsc[tg.group][l] = x
				}
			}
		}
		for g, name := range groups {
			if failed[g] || len(gms[g]) == 0 {
				fmt.Printf("  verdict %-26s incomplete\n", name)
				continue
			}
			sortD(gms[g])
			v := verdict{arm: a.name, name: name, fs: map[string]int{}, med: gms[g][len(gms[g])/2]}
			v.pass = v.med <= maxMedian
			for l, x := range gsc[g] {
				v.ok += x.ok
				v.n += x.n
				v.fs[l] = x.falseSpeak
				v.pass = v.pass && x.falseSpeak <= maxFalseSpeak
			}
			v.pass = v.pass && v.ok*20 >= passCorrect*v.n
			fmt.Printf("  verdict %-26s %d/%d  fs", name, v.ok, v.n)
			for _, l := range langs {
				if x, ok := v.fs[l]; ok {
					fmt.Printf(" %s %d", l, x)
				}
			}
			fmt.Printf("  median %d ms  pass=%v\n", v.med.Milliseconds(), v.pass)
			if v.pass && (best == nil || better(v, *best)) {
				best = &v
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
