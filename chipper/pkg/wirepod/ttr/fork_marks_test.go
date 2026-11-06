package wirepod_ttr

import (
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestForkSpecialCharsEstonianKeepsDiacritics(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	in := "Tänase nädalapäeva võin öelda: üks šokolaad ja žürii."
	if got := removeSpecialCharacters(in); got != in {
		t.Errorf("removeSpecialCharacters(%q) = %q", in, got)
	}
}

func TestForkSpecialCharsEnglishUnchanged(t *testing.T) {
	lang.UseForTest(t, lang.English)
	in := "Tänase öelda šokolaad café"
	want := "Tanase oelda sokolaad café" // é (U+0301) is in upstream's Vietnamese keep list
	if got := removeSpecialCharacters(in); got != want {
		t.Errorf("removeSpecialCharacters(%q) = %q, want %q", in, got, want)
	}
}
