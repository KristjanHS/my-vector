// Package prompt renders the fork's system-prompt blocks (2026-11-07-system-prompt-redesign.md (docs/plans/)).
package prompt

import (
	"fmt"
	"time"
	_ "time/tzdata" // robot zone names resolve even without host zoneinfo

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

var (
	etWeekdays = [...]string{"pühapäev", "esmaspäev", "teisipäev", "kolmapäev", "neljapäev", "reede", "laupäev"}
	etMonths   = [...]string{"jaanuar", "veebruar", "märts", "aprill", "mai", "juuni", "juuli", "august", "september", "oktoober", "november", "detsember"}
)

// Zone loads the IANA zone name, else time.Local.
func Zone(name string) *time.Location {
	if name == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

// Now is the {now} block: weekday, date and time of t in loc, in language l (et-EE native, else English).
func Now(l string, t time.Time, loc *time.Location) string {
	t = t.In(loc)
	zone := loc.String()
	if loc == time.Local {
		zone = t.Format("MST") // "Local" tells the model nothing
	}
	if l == lang.Estonian {
		return fmt.Sprintf("Praegu on %s, %d. %s %d, kell %d.%02d (%s).",
			etWeekdays[t.Weekday()], t.Day(), etMonths[t.Month()-1], t.Year(), t.Hour(), t.Minute(), zone)
	}
	return fmt.Sprintf("It is %s, %d %s %d, %02d:%02d (%s).", t.Weekday(), t.Day(), t.Month(), t.Year(), t.Hour(), t.Minute(), zone)
}
