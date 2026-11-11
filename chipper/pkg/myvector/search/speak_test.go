package search

import (
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func testForecast() Forecast {
	var f Forecast
	f.Current.Temp, f.Current.Code = 3.2, 3
	f.Daily.Time = []string{"2026-11-11", "2026-11-12", "2026-11-13"}
	f.Daily.Code = []int{51, 51, 73}
	f.Daily.Min, f.Daily.Max = []float64{1.9, 1.9, -6.2}, []float64{3.6, 5.7, -1.8}
	f.Daily.Precip, f.Daily.PrecipP = []float64{0.1, 0.2, 3.5}, []int{53, 83, 78}
	return f
}

func TestSpeakWeather(t *testing.T) {
	f := testForecast()
	for _, c := range []struct{ ask, in, want string }{
		{"kas homme sajab lund Tallinnas?", "Tallinnas",
			"Ei, homme lund ei tule. Homme on Tallinnas kaks kuni kuus kraadi sooja, uduvihm, sademete tõenäosus kaheksakümmend kolm protsenti."},
		{"kui külm homme Tallinnas on?", "Tallinnas",
			"Homme on Tallinnas kaks kuni kuus kraadi sooja, uduvihm, sademete tõenäosus kaheksakümmend kolm protsenti."},
		{"kas ülehomme tuleb lund?", "",
			"Jah, ülehomme tuleb lund. Tallinn, ülehomme on kaks kuni kuus kraadi külma, lumesadu, sademete tõenäosus seitsekümmend kaheksa protsenti."},
		{"kas täna vihma tuleb?", "Tallinnas",
			"Jah, täna tuleb vihma. Praegu on Tallinnas kolm kraadi sooja, pilves. Täna on kaks kuni neli kraadi sooja, uduvihm, sademete tõenäosus viiskümmend kolm protsenti."},
		{"kas homme tuleb vihma või lund?", "Tallinnas",
			"Jah, homme sajab. Homme on Tallinnas kaks kuni kuus kraadi sooja, uduvihm, sademete tõenäosus kaheksakümmend kolm protsenti."},
	} {
		d, ok := replyDay(tokens(c.ask))
		if !ok {
			t.Fatalf("%q: no day", c.ask)
		}
		if got := speakWeather(tokens(c.ask), d, "Tallinn", c.in, f); got != c.want {
			t.Errorf("%q:\n got %q\nwant %q", c.ask, got, c.want)
		}
	}
}

func TestReplyDay(t *testing.T) {
	for ask, want := range map[string]int{"mis ilm on?": 0, "homme hommikul": 1, "homseks": 1, "ülehomme": 2, "hommikul": 0} {
		if d, ok := replyDay(tokens(ask)); !ok || d != want {
			t.Errorf("%q: day %d %v, want %d", ask, d, ok, want)
		}
	}
	for _, ask := range []string{"mis ilm reedel on?", "ilm nädalavahetusel", "kas pühapäeval sajab?", "kui tugev tuul homme on?",
		"kas kolme päeva pärast sajab?", "kas 15. novembril sajab?", "kas järgmisel päeval sajab?"} {
		if _, ok := replyDay(tokens(ask)); ok {
			t.Errorf("%q: want no day (past the template's reach)", ask)
		}
	}
}

func TestEtTemps(t *testing.T) {
	for _, c := range []struct {
		lo, hi int
		want   string
	}{{2, 6, "kaks kuni kuus kraadi sooja"}, {-6, -2, "kaks kuni kuus kraadi külma"}, {-1, 2, "miinus üks kuni pluss kaks kraadi"},
		{0, 0, "null kraadi"}, {-3, 0, "null kuni kolm kraadi külma"}, {14, 21, "neliteist kuni kakskümmend üks kraadi sooja"}} {
		if got := etTemps(c.lo, c.hi); got != c.want {
			t.Errorf("etTemps(%d, %d) = %q, want %q", c.lo, c.hi, got, c.want)
		}
	}
}

func TestWeatherReply(t *testing.T) {
	tln, _ := time.LoadLocation("Europe/Tallinn")
	qs := fakeOpenMeteo(t)
	useConfig(t, `{}`)
	in := func(string) string { return "Tallinnas" }
	if _, ok := WeatherReply("what's the weather tomorrow?", lang.English, in, tln); ok || len(*qs) != 0 {
		t.Errorf("en: want no reply and no lookup, got ok=%v after %d calls", ok, len(*qs))
	}
	for _, ask := range []string{"räägi nali", "will it rain tomorrow?"} {
		if _, ok := WeatherReply(ask, lang.Estonian, in, tln); ok {
			t.Errorf("%q in et mode: want no reply", ask)
		}
	}
	old := weatherClock
	t.Cleanup(func() { weatherClock = old })
	weatherClock = func() time.Time { return time.Date(2026, 11, 7, 10, 0, 0, 0, tln) } // forecastJS's day 0
	if _, ok := WeatherReply("kas homme sajab lund?", lang.Estonian, in, tln); ok || len(*qs) != 0 {
		t.Errorf("cold memo: want no reply and no lookup, got ok=%v after %d calls", ok, len(*qs))
	}
	Fill("kas homme sajab lund?", nil, lang.Estonian, prompt.OllamaEt, tln)
	got, ok := WeatherReply("kas homme sajab lund?", lang.Estonian, in, tln)
	if !ok || !strings.HasPrefix(got, "Ei, homme lund ei tule. Homme on Tallinnas ") || len(*qs) != 2 {
		t.Fatalf("et weather ask after Fill: %q %v after %d calls, want the block's 2", got, ok, len(*qs))
	}
	weatherClock = func() time.Time { return time.Date(2026, 11, 8, 0, 10, 0, 0, tln) } // past midnight, same memo
	if got, ok := WeatherReply("kas homme sajab lund?", lang.Estonian, in, tln); !ok || !strings.HasPrefix(got, "Jah, homme tuleb lund. ") ||
		!strings.Contains(got, "kuni pluss üks kraadi") {
		t.Errorf("after midnight: %q %v, want 9.11's snow (-3.5..1.0), not 8.11's rain", got, ok)
	}
	if got := Fill("mis ilm homme on?", nil, lang.Estonian, prompt.Ollama, tln); !strings.Contains(got, "\nToday: 2 to 6°C, light rain") ||
		!strings.Contains(got, "\nTomorrow: -4 to 1°C, light snow") || len(*qs) != 2 {
		t.Errorf("block after midnight, same memo: %q after %d calls, want 8.11 as Today and 9.11 as Tomorrow", got, len(*qs))
	}
	if _, ok := WeatherReply("kui külm on Tartus homme?", lang.Estonian, in, tln); ok {
		t.Error("another town: want no reply")
	}
	if n := len(current().Cache); n != 0 {
		t.Errorf("weather block in the persisted cache (%d entries): the memo is its cache", n)
	}
}

func TestSpeakWeatherMaybe(t *testing.T) {
	f := testForecast()
	f.Daily.PrecipP[1] = 30
	for ask, want := range map[string]string{
		"kas homme vihma tuleb?":          "Homme võib tulla vihma. ",
		"kas homme sajab?":                "Homme võib sadada. ",
		"kas homme tuleb vihma või lund?": "Homme võib sadada. ",
		"kas homme lund tuleb?":           "Ei, homme lund ei tule. ",
	} {
		if got := speakWeather(tokens(ask), 1, "Tallinn", "Tallinnas", f); !strings.HasPrefix(got, want+"Homme on Tallinnas ") {
			t.Errorf("%q at 30%%: got %q, want prefix %q", ask, got, want)
		}
	}
}

func TestNamesOtherPlace(t *testing.T) {
	for ask, want := range map[string]bool{
		"Kas homme sajab Tallinnas?":    false,
		"kas homme sajab lund?":         false,
		"Kui külm on Tartus homme?":     true,
		"kas tartus homme sajab?":       true,
		"Kas homme sajab Oslos?":        true,
		"kas homme sajab kuopios?":      true,
		"kas homme sajab, vector?":      false,
		"mis ilm riias homme on?":       true,
		"kas homme kärdlas sajab?":      true,
		"kas ma viin homme vihmavarju":  false,
		"mis ilm on homme soomes?":      true,
		"kas homme sajab new yorgis?":   true,
		"kas homme sajab elvas?":        true,
		"mis ilm on lõuna-eestis?":      true,
		"kas pargis on täna märg?":      false,
		"kas homme tuleb päikest?":      false,
		"kas homme pärastlõunal sajab?": false,
		"kas rannas on homme soe?":      false,
		"kas turul on homme vihma?":     false,
		"kas lahti jopega saab?":        false,
	} {
		if got := namesOtherPlace(ask, "Tallinn", "Tallinn", "Tallinnas"); got != want {
			t.Errorf("namesOtherPlace(%q) = %v, want %v", ask, got, want)
		}
	}
	if !namesOtherPlace("kas Tallinnas sajab?", "Tartu", "Tartu", "Tartus") {
		t.Error("home Tartu: a Tallinn ask must skip the template")
	}
}

func TestFromToday(t *testing.T) {
	f, ok := fromToday(testForecast(), "2026-11-12") // memoised before midnight: day 0 is yesterday
	if !ok || len(f.Daily.Time) != 2 || f.Daily.Time[0] != "2026-11-12" || f.Daily.Code[1] != 73 || f.Daily.PrecipP[0] != 83 {
		t.Errorf("fromToday(12.11) = %+v %v", f.Daily, ok)
	}
	if _, ok := fromToday(testForecast(), "2026-11-20"); ok {
		t.Error("a day past the forecast: want false")
	}
	tln, _ := time.LoadLocation("Europe/Tallinn")
	at := time.Date(2026, 11, 11, 22, 30, 0, 0, time.UTC) // 00:30 next day in Tallinn
	if got := placeToday(Place{Timezone: "Europe/Tallinn"}, time.UTC, at); got != "2026-11-12" {
		t.Errorf("placeToday(Tallinn) = %s, want 2026-11-12", got)
	}
	if got := placeToday(Place{}, tln, at); got != "2026-11-12" {
		t.Errorf("placeToday(no tz, zone Tallinn) = %s, want 2026-11-12", got)
	}
	for _, tz := range []string{"", "Not/AZone"} {
		if got, want := placeToday(Place{Timezone: tz}, nil, at), at.In(time.Local).Format(time.DateOnly); got != want {
			t.Errorf("placeToday(tz %q, nil zone) = %s, want local %s", tz, got, want)
		}
	}
}
