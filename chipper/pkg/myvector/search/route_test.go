package search

import (
	"os"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestRoute(t *testing.T) {
	et, en := lang.Estonian, lang.English
	for _, c := range []struct {
		s, l string
		want Class
	}{
		{"mis ilm homme on?", et, Weather},
		{"kas homme sajab?", et, Weather},
		{"milline on ilma prognoos?", et, Weather},
		{"what's the weather tomorrow?", en, Weather},
		{"kes võitis eile jalgpallis?", et, Live},
		{"mis on euro kurss?", et, Live},
		{"kui palju maksab bitcoin?", et, Live},
		{"millal Rimi lahti on?", et, Live},
		{"latest news", en, Live},
		{"kes on Eesti peaminister?", et, Recent},
		{"mis juhtus täna Tallinnas?", et, Recent},
		{"mis täna Eestis juhtus?", et, Recent},
		{"mis siis juhtus?", et, None},
		{"mis juhtus?", et, None},
		{"mis siis nüüd juhtus?", et, None},
		{"mis nüüd jälle juhtus?", et, None},
		{"mis siis lõpuks juhtus?", et, None},
		{"mis selle autoga juhtus", et, None},
		{"mis selle filmiga lõpus juhtus?", et, None},
		{"mis kell koosolek toimub?", et, None},
		{"mis edasi juhtus muinasjutus?", et, None},
		{"who is the president of Finland?", en, Recent},
		{"what happened in Tartu today?", en, Recent},
		{"Who’s the CEO of Bolt?", en, Recent},
		{"mis on fotosüntees?", et, None},
		{"räägi nali", et, None},
		{"what is photosynthesis?", en, None},
		{"tell me a joke", en, None},
		{"kuidas sul läheb?", et, None},
		{"ilma suhkruta palun", et, None},
		{"see on ilmselt hea", et, None},
		{"kes on sinu lemmik?", et, None},
		{"who is your best friend?", en, None},
		{"what's the weather tomorrow?", "fr-FR", Weather},
		// Weak weather words need a weather-context token; strong cues ignore the self guard.
		{"how hot is it outside", en, Weather},
		{"is it cold outside", en, Weather},
		{"mis kraad õues on", et, Weather},
		{"kas homme on soe?", et, Weather},
		{"can you tell me the weather tomorrow?", en, Weather},
		{"kas sa tead, mis ilm homme on?", et, Weather},
		{"when does the store close", en, Live},
		{"is Rimi open today?", en, Live},
		{"millal pood kinni läheb", et, Live},
		{"kui palju bensiin maksab?", et, Live},
		{"latest football results", en, Live},
		// Command cues fire past the self guard, with no object block (R1); "otsin" is not "otsi".
		{"otsi viimase pakkumise pealt, number kolm.", et, Live},
		{"otsi mulle uudiseid kiipidest", et, Live},
		{"vaata palun järele, millal Rimi avatakse", et, Live},
		{"otsi kuubik", et, Live},
		{"otsin prille", et, None},
		{"search for semiconductor news", en, Live},
		{"can you look it up for me?", en, Live},
		{"look up at me", en, None},
		{"look up", en, None},
		{"look up the opening hours of Rimi", en, Live},
		{"look up the population of Estonia", en, Live},
		{"look that up", en, Live},
		{"look up a little", en, None},
		{"look up a bit", en, None},
		{"look up the ceiling", en, None},
		{"look up, what is that?", en, None},
		{"look up, where is the ball", en, None},
		{"can you look up when Rimi opens", en, None}, // no object pronoun or "the * of": a head move reads the same
		{"find out who runs Bolt", en, Live},
		{"can you find out when Rimi opens?", en, Live},
		{"let's find out what happens", en, None},
		{"you'll find out soon", en, None},
		{"i want to find out if you can dance", en, None},
		// An et turn falls back to the English cues (F2).
		{"search for present news about semiconductors related financial.", et, Live},
		{"what's the weather tomorrow?", et, Weather},
		{"who won the election?", en, Live},
		{"kas sa oled soe", et, None},
		{"mul on külm", et, None},
		{"soe tee", et, None},
		{"kes on Tuuli", et, None},
		{"Lumi on mu kass", et, None},
		{"lähme sadama juurde", et, None},
		{"vihmauss", et, None},
		{"vihmavari on kodus", et, None},
		{"tuuleveski", et, None},
		{"prognoosi järgi läheb hästi", et, None},
		{"kraadiklaas", et, None},
		{"tell me about snow white", en, None},
		{"rain man is a good film", en, None},
		{"what is your body temperature", en, None},
		{"how many degrees in a circle", en, None},
		{"temperature of the sun", en, None},
		{"open the door", en, None},
		{"can you open your eyes", en, None},
		{"is your mouth open?", en, None},
		{"mis mu tulemus on?", et, None},
		{"i won the game", en, None},
		{"we won", en, None},
		{"what does it cost you", en, None},
		{"võta see lahti", et, None},
		{"ma saan sellest lahti", et, None},
		{"see maksab kätte", et, None},
		{"kas sa oled kursis", et, None},
		{"tulemuseks oli hea", et, None},
		{"mis toimub?", et, None},
		{"mis juhtus?", et, None},
		{"what's happening", en, None},
		{"who's there", en, None},
		{"who is he", en, None},
		{"who is this", en, None},
		{"kes on ta", et, None},
		{"who is the robot i'm talking to", en, None},
	} {
		if got := Route(c.s, c.l); got != c.want {
			t.Errorf("Route(%q, %s) = %v, want %v", c.s, c.l, got, c.want)
		}
	}
}

// The English fallback adds no hit to an Estonian sentence (testdata/et_corpus.txt: route_test, probe and bench sets).
func TestRouteEnglishFallbackCorpus(t *testing.T) {
	b, err := os.ReadFile("testdata/et_corpus.txt")
	if err != nil {
		t.Fatal(err)
	}
	if Route("kas sa tead mis google on", lang.Estonian) != Live || route("kas sa tead mis google on", lang.Estonian) != None {
		t.Error("control: an English command cue in an et sentence must reach Live only through the fallback")
	}
	n := 0
	for _, s := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		n++
		if got, et := Route(s, lang.Estonian), route(s, lang.Estonian); got != et {
			t.Errorf("%q: fallback %v, Estonian cues %v", s, got, et)
		}
	}
	if n < 150 {
		t.Fatalf("corpus has %d sentences, want ≥150", n)
	}
}
