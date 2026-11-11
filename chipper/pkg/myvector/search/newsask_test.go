package search

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestNewsAsk(t *testing.T) {
	et := lang.Estonian
	news, rimi := "otsi uudiseid Nvidia aktsia kohta", "millal Rimi lahti on?"
	for _, c := range []struct {
		sentence string
		prev     []string
		class    Class
		want     bool
	}{
		{news, nil, Live, true},
		{rimi, nil, Live, false},
		{"otsi seda", []string{news}, Live, true}, // News carried from the referred-back ask
		{"otsi seda", []string{rimi}, Live, false},
		{"kas homme sajab? räägi ilmauudiseid", nil, Weather, false},
		{"räägi nali", nil, None, false},
	} {
		if got := Route(c.sentence, et); got != c.class {
			t.Errorf("Route(%q) = %v, want %v (case precondition)", c.sentence, got, c.class)
		}
		if got := NewsAsk(c.sentence, c.prev, et); got != c.want {
			t.Errorf("NewsAsk(%q, %q) = %v, want %v", c.sentence, c.prev, got, c.want)
		}
	}
}
