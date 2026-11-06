package wirepod_ttr

import "github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"

// forkMarkFilter is the combining-mark filter removeSpecialCharacters strips with (kgsim.go).
// Upstream isMn drops every mark but a Vietnamese set, so "Tänase … öelda" reached the TTS as
// "Tanase … oelda". On et-EE the Estonian alphabet's marks stay too: diaeresis (ä ö ü) and
// caron (š ž); õ's tilde is already in isMn's keep list. English is unchanged.
func forkMarkFilter() func(rune) bool {
	if lang.Current() != lang.Estonian {
		return isMn
	}
	return func(r rune) bool {
		if r == '̈' || r == '̌' {
			return false
		}
		return isMn(r)
	}
}
