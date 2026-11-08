package main

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func TestChatSets(t *testing.T) {
	want := map[string]map[string]int{"dev": {"en-US": 16, "et-EE": 16}, "holdout": {"en-US": 12, "et-EE": 12}}
	seen := map[string]string{}
	for set, sizes := range want {
		got := map[string]int{}
		for _, a := range chatSets[set] {
			got[a.Lang]++
			if prev, ok := seen[a.Text]; ok {
				t.Errorf("%s: %q also in %s", set, a.Text, prev)
			}
			seen[a.Text] = set
			if long := vars.ForkLongAsk(a.Text); long != (a.Kind == "long") {
				t.Errorf("%s: %q kind %s, ForkLongAsk %v", set, a.Text, a.Kind, long)
			}
		}
		for l, n := range sizes {
			if got[l] != n {
				t.Errorf("%s %s: %d asks, want %d", set, l, got[l], n)
			}
		}
		if len(got) != len(sizes) {
			t.Errorf("%s languages %v, want %v", set, got, sizes)
		}
	}
}
