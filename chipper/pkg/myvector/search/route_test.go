package search

import (
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
