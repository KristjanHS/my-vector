package followup

import "testing"

func TestRepeatFilter(t *testing.T) {
	said := []string{"Muidugi. Kas sinuk on mingid eriti huvilised tegurid? See on hea."}
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
	got := Questions("Kell on 3.15. Kas sobib? Jah! Mis veel?")
	if len(got) != 2 || got[0] != "Kas sobib?" || got[1] != "Mis veel?" {
		t.Errorf("Questions = %q", got)
	}
}
