package wirepod_ttr

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	lcztn "github.com/kercre123/wire-pod/chipper/pkg/wirepod/localization"
)

// forkTimerUnits: nominative, genitive, translative, partitive → seconds (E23).
var forkTimerUnits = map[string]int{
	"sekund": 1, "sekundi": 1, "sekundiks": 1, "sekundit": 1,
	"minut": 60, "minuti": 60, "minutiks": 60, "minutit": 60,
	"tund": 3600, "tunni": 3600, "tunniks": 3600, "tundi": 3600,
}

var forkTimerHalf = map[string]bool{"pool": true, "poole": true, "pooleks": true}

// forkTimerNumbers maps every 1–99 word form E23 names (unit, teen, ten; nom/gen/-ks) to its value.
var forkTimerNumbers = func() map[string]int {
	digits := [][2]string{{"üks", "ühe"}, {"kaks", "kahe"}, {"kolm", "kolme"}, {"neli", "nelja"}, {"viis", "viie"},
		{"kuus", "kuue"}, {"seitse", "seitsme"}, {"kaheksa", "kaheksa"}, {"üheksa", "üheksa"}}
	m := map[string]int{"kümme": 10, "kümne": 10, "kümneks": 10}
	for i, f := range digits {
		d, nom, gen := i+1, f[0], f[1]
		m[nom], m[gen], m[gen+"ks"] = d, d, d
		m[nom+"teist"], m[gen+"teistkümne"], m[gen+"teistkümneks"] = 10+d, 10+d, 10+d
		if d > 1 {
			m[nom+"kümmend"], m[gen+"kümne"], m[gen+"kümneks"] = 10*d, 10*d, 10*d
		}
	}
	return m
}()

// forkTimerText rewrites an et-EE timer phrase to "<seconds> second", the one form both words2num branches parse.
func forkTimerText(input string) string {
	if lang.Current() != lang.Estonian {
		return input
	}
	words := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	total, num, half := 0, 0, false
	for _, w := range words {
		if secs, ok := forkTimerUnits[w]; ok {
			if half {
				total += secs / 2
			} else {
				total += num * secs
			}
			num, half = 0, false
		} else if n, err := strconv.Atoi(w); err == nil {
			num = n
		} else if n, ok := forkTimerNumbers[w]; ok {
			if num >= 20 && num%10 == 0 && n < 10 {
				num += n // "kakskümmend viis", "kahekümne viieks"
			} else {
				num = n
			}
		} else if forkTimerHalf[w] {
			half = true
		}
	}
	if total == 0 {
		return input
	}
	return strconv.Itoa(total) + " " + lcztn.GetText(lcztn.STR_SECOND)
}
