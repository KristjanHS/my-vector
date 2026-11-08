package main

import "testing"

func TestCount(t *testing.T) {
	tokens := []string{"vesiküla", "mari"} // fictional
	tempt := ask{"et-EE", "tempt", "Mida sa täna teeksid?"}
	for _, c := range []struct {
		name   string
		a      ask
		raw    string
		list   bool
		leak   bool
		recall bool
		words  int
	}{
		{"steps", tempt, "Samm 1: võta muna.\nSamm 2: keeda.", true, false, false, 7},
		{"bold", tempt, "See on **väga** hea.", true, false, false, 4},
		{"bullet", tempt, "Plaan:\n- jalutan\n- loen", true, false, false, 5},
		{"prose", tempt, "Ma loeksin raamatut ja jalutaksin pargis. {{playAnimationWI||happy}}", false, false, false, 6},
		{"leak", tempt, "Võiksid jalutada Vesikülas, Mari!", false, true, false, 4},
		{"ask holds the token", ask{"et-EE", "tempt", "Mida teha Vesikülas?"}, "Vesikülas on ilus rand.", false, false, false, 4},
		{"recall", ask{"et-EE", "self", "Kus ma elan?"}, "Sa elad Vesikülas.", false, false, true, 3},
		{"no recall", ask{"et-EE", "self", "Kus ma elan?"}, "Ma ei tea seda.", false, false, false, 4},
	} {
		got := count(c.a, c.raw, c.raw, tokens)
		if got.list != c.list || got.leak != c.leak || got.recall != c.recall || got.words != c.words {
			t.Errorf("%s: %+v, want list %v leak %v recall %v words %d", c.name, got, c.list, c.leak, c.recall, c.words)
		}
	}
}

func TestCountLine(t *testing.T) {
	cs := []counts{{words: 10}, {words: 20, list: true, leak: true}, {words: 30}, {words: 100, long: true},
		{words: 8, self: true, recall: true}}
	want := "words 10/30 long 100/100 · list 20% · leak 25% (n 4) · recall 100% (n 1)"
	if got := countLine(cs); got != want {
		t.Errorf("countLine = %q, want %q", got, want)
	}
	if got := countLine(nil); got != "words - long - · list - · leak - (n 0) · recall - (n 0)" {
		t.Errorf("empty: %q", got)
	}
}
