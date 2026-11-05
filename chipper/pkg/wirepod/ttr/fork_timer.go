package wirepod_ttr

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	lcztn "github.com/kercre123/wire-pod/chipper/pkg/wirepod/localization"
)

// forkTimerUnits: nominative, genitive, translative, partitive → seconds (E23).
var forkTimerUnits = map[string]float64{
	"sekund": 1, "sekundi": 1, "sekundiks": 1, "sekundit": 1,
	"minut": 60, "minuti": 60, "minutiks": 60, "minutit": 60,
	"tund": 3600, "tunni": 3600, "tunniks": 3600, "tundi": 3600,
}

// forkTimerFractions add to the number before them: "kaks ja pool" = 2.5, "poolteist" = 1.5.
var forkTimerFractions = map[string]float64{
	"pool": 0.5, "poole": 0.5, "pooleks": 0.5,
	"poolteist": 1.5, "pooleteist": 1.5, "pooleteise": 1.5, "pooleteiseks": 1.5,
}

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

// forkTimerToken keeps a decimal comma inside its number ("1,5") instead of splitting it.
var forkTimerToken = regexp.MustCompile(`\d+(?:[.,]\d+)?|\pL+`)

// forkTimerText rewrites an et-EE timer phrase to "<seconds> <second unit>", the one form both words2num branches parse.
func forkTimerText(input string) string {
	if lang.Current() != lang.Estonian {
		return input
	}
	total, num := 0.0, 0.0
	for _, w := range forkTimerToken.FindAllString(strings.ToLower(input), -1) {
		if secs, ok := forkTimerUnits[w]; ok {
			if num == 0 {
				num = 1 // bare unit: "pane taimer tunniks"
			}
			total += num * secs
			num = 0
		} else if w[0] >= '0' && w[0] <= '9' { // digits only: ParseFloat also takes "inf"/"nan"
			num, _ = strconv.ParseFloat(strings.Replace(w, ",", ".", 1), 64)
		} else if n, ok := forkTimerNumbers[w]; ok {
			if num >= 20 && num < 100 && math.Mod(num, 10) == 0 && n < 10 {
				num += float64(n) // "kakskümmend viis", "kahekümne viieks"
			} else {
				num = float64(n)
			}
		} else if f, ok := forkTimerFractions[w]; ok {
			num += f
		}
	}
	secs := int(math.Round(total))
	if secs == 0 {
		return input
	}
	unit := lcztn.GetText(lcztn.STR_SECOND) // the vosk branch's regex unit
	if os.Getenv("STT_SERVICE") == "whisper.cpp" {
		unit = "second" // whisperSpeechtoNum hard-codes it; ko-KR's "초" would never match
	}
	return strconv.Itoa(secs) + " " + unit
}
