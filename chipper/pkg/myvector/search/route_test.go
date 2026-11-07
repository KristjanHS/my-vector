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
	} {
		if got := Route(c.s, c.l); got != c.want {
			t.Errorf("Route(%q, %s) = %v, want %v", c.s, c.l, got, c.want)
		}
	}
}
