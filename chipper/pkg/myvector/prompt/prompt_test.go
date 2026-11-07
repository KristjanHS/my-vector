package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

func TestNowGolden(t *testing.T) {
	tallinn := Zone("Europe/Tallinn")
	at := time.Date(2026, 11, 7, 7, 43, 0, 0, time.UTC)
	for _, c := range []struct{ l, want string }{
		{lang.Estonian, "Praegu on laupäev, 7. november 2026, kell 9.43 (Europe/Tallinn)."},
		{lang.English, "It is Saturday, 7 November 2026, 09:43 (Europe/Tallinn)."},
	} {
		if got := Now(c.l, at, tallinn); got != c.want {
			t.Errorf("Now(%s) = %q, want %q", c.l, got, c.want)
		}
	}
	// Sunday = etWeekdays[0]; 22:05 UTC Saturday is already Sunday in Tallinn.
	if got := Now(lang.Estonian, time.Date(2026, 11, 7, 22, 5, 0, 0, time.UTC), tallinn); got != "Praegu on pühapäev, 8. november 2026, kell 0.05 (Europe/Tallinn)." {
		t.Errorf("zone shift: %q", got)
	}
}

func TestZoneFallback(t *testing.T) {
	for _, name := range []string{"", "Not/AZone"} {
		if Zone(name) != time.Local {
			t.Errorf("Zone(%q) != time.Local", name)
		}
	}
	if got := Now(lang.English, time.Now(), time.Local); strings.Contains(got, "Local") {
		t.Errorf("fallback zone printed as Local: %q", got)
	}
}
