package vars

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestForkLongAsk(t *testing.T) {
	for _, c := range []struct {
		lang string
		hit  []string
		miss []string
	}{
		{lang.English,
			[]string{"Tell me a story about a robot", "any bedtime stories", "read me a fairy tale", "explain it in detail",
				"give me a detailed answer", "show me step by step", "answer in ten sentences", "write two paragraphs"},
			[]string{"how much storage is left", "tell me some history", "who is a storyteller", "what is the capital of france"}},
		{lang.Estonian,
			[]string{"räägi mulle lugu", "tea mõned lugusid", "jutusta midagi", "räägi mulle muinasjutt robotist",
				"seleta pikemalt", "anna üksikasjalikum vastus", "räägi põhjalikult", "näita samm-sammult",
				"vasta kümne lause asemel viis lauset", "kirjuta kaks lõiku"},
			[]string{"räägi ajalugu", "vasta kahe lausega", "mis kell on"}},
	} {
		lang.UseForTest(t, c.lang)
		for _, s := range c.hit {
			if !ForkLongAsk(s) {
				t.Errorf("%s: %q not a long ask", c.lang, s)
			}
		}
		for _, s := range c.miss {
			if ForkLongAsk(s) {
				t.Errorf("%s: %q is a long ask", c.lang, s)
			}
		}
	}
}
