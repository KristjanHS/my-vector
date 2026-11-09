package followup

import (
	"strings"
	"testing"
)

func TestRepeatFilter(t *testing.T) {
	said := []string{"Muidugi. Kas sinuk on mingid eriti huvilised tegurid? See on hea.", "Mida? Mis sa arvad? Kas sa soovid midagi?"}
	for _, c := range []struct {
		name, s string
		want    bool
	}{
		{"same question", "Kas sinuk on mingid eriti huvilised tegurid?", true},
		{"today variant", "Kas sinuk on täna mingid eriti huvilised tegurid?", true},
		{"after-that variant", "Kas sinuk on mingid eriti huvilised tegurid pärast seda?", true},
		{"distinct question sharing words", "Kas sinuk on mingid lemmikloomad?", false},
		{"repeated statement", "Kas sinuk on mingid eriti huvilised tegurid.", false},
		{"no history match", "Mis kell on?", false},
		{"overlap exactly RepeatMin", "Kas sinuk on täna mingid eriti lihtsad tegurid?", true}, // 6/8
		{"question in quotes", "Kas sinuk on mingid eriti huvilised tegurid?\"", true},
		{"1 word under RepeatWords", "Mida?", false},
		{"3 words under RepeatWords", "Mis sa arvad?", false},
		{"4 words at RepeatWords", "Kas sa soovid midagi?", true},
	} {
		if got := Repeats(c.s, said, RepeatMin); got != c.want {
			t.Errorf("%s: Repeats(%q) = %v", c.name, c.s, got)
		}
	}
}

func TestOverlap(t *testing.T) {
	a := "Kas sinuk on mingid eriti huvilised tegurid?"
	if got := Overlap(a, "Kas sinuk on täna mingid eriti huvilised tegurid?"); got != 7.0/8 {
		t.Errorf("today variant = %v, want 0.875", got)
	}
	if got := Overlap("", ""); got != 0 {
		t.Errorf("empty = %v", got)
	}
}

func TestQuestions(t *testing.T) {
	for msg, want := range map[string][]string{
		"Kell on 3.15. Kas sobib? Jah! Mis veel?": {"Kas sobib?", "Mis veel?"},
		`Ta ütles "tere." Kas sa tuled?`:          {"Kas sa tuled?"},
		"Kas tõesti?! Jah.":                       {"Kas tõesti?!"},
		`Ta küsis "kas sa tuled?" ja läks.`:       {`Ta küsis "kas sa tuled?"`},
	} {
		got := Questions(msg)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("Questions(%q) = %q, want %q", msg, got, want)
		}
	}
}
