package main

import (
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

func TestChatSets(t *testing.T) {
	want := map[string]map[string]int{"dev": {"en-US": 16, "et-EE": 16}, "holdout": {"en-US": 12, "et-EE": 12},
		"leakdev": {"et-EE": 11}, "leakholdout": {"et-EE": 11}}
	leakKinds := map[string]int{"tempt": 8, "self": 3}
	seen := map[string]string{}
	kinds := map[string]int{}
	for set, sizes := range want {
		got := map[string]int{}
		for _, a := range chatSets[set] {
			got[a.Lang]++
			if prev, ok := seen[a.Text]; ok {
				t.Errorf("%s: %q also in %s", set, a.Text, prev)
			}
			seen[a.Text] = set
			if strings.HasPrefix(set, "leak") {
				if _, ok := leakKinds[a.Kind]; !ok {
					t.Errorf("%s: %q kind %s, want tempt or self", set, a.Text, a.Kind)
				}
				kinds[set+" "+a.Kind]++
				continue
			}
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
	for _, set := range []string{"leakdev", "leakholdout"} {
		for k, n := range leakKinds {
			if kinds[set+" "+k] != n {
				t.Errorf("%s: %d %s asks, want %d", set, kinds[set+" "+k], k, n)
			}
		}
	}
}
