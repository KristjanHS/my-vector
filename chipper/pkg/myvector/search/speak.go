package search

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// WeatherReply is the spoken et-EE answer to a Weather ask, built from Open-Meteo without the brain (N11: the et brain
// refused ~3/8 with the forecast in its prompt); false for another language, another town, a day past the forecast,
// or no forecast in the memo. It reads only the memo Fill's {search} lookup filled this turn (N12: no second wait).
// inessive is the place name's "-s" form ("Tallinnas"), "" when unknown.
func WeatherReply(sentence, l string, inessive func(string) string, zone *time.Location) (string, bool) {
	if l != lang.Estonian || route(sentence, l) != Weather { // not Route: its English fallback would take "rain tomorrow"
		return "", false
	}
	toks := tokens(sentence)
	d, ok := replyDay(toks)
	if !ok {
		return "", false
	}
	place := weatherPlace(LoadConfig(), zone)
	p, f, ok := current().memoGet(place, time.Now())
	if !ok || namesOtherPlace(sentence, place, p.Name, inessive(p.Name)) {
		return "", false
	}
	f, ok = fromToday(f, placeToday(p, zone, weatherClock()))
	if !ok || d >= len(f.Daily.Time) || d >= len(f.Daily.Code) || d >= len(f.Daily.Min) || d >= len(f.Daily.Max) {
		return "", false
	}
	return speakWeather(toks, d, p.Name, inessive(p.Name), f), true
}

// weatherClock dates WeatherReply's "today"; a var so tests pin it to the fixture's days.
var weatherClock = time.Now

// namesOtherPlace: the ask names a place other than the forecast place (homes: its names, any case) — a known
// place's stem (Q8 A: et STT is lower-case, so no capital-letter rule). A false skip only costs the LLM path.
func namesOtherPlace(sentence string, homes ...string) bool {
	for _, w := range strings.FieldsFunc(sentence, func(r rune) bool { return !unicode.IsLetter(r) && r != '-' }) {
		lw := strings.ToLower(w)
		if asks(homes, func(h string) bool { return h != "" && strings.HasPrefix(lw, strings.ToLower(h)) }) {
			continue
		}
		if asks(etTowns, func(t string) bool { return strings.HasPrefix(lw, t) }) {
			return true
		}
	}
	return false
}

// etTowns are place stems an et ask inflects ("tartus", "riiga"); no stem that is also a common word ("tapa", "viin",
// "lahti", "turu", "turus").
var etTowns = []string{"tallinn", "tartu", "pärnu", "narva", "viljandi", "rakvere", "kuressaare", "haapsalu", "võru",
	"valga", "jõhvi", "paide", "rapla", "kärdla", "põlva", "jõgeva", "sillamäe", "maardu", "keila", "kohtla", "otepää",
	"saaremaa", "hiiumaa", "elva", "türi", "kehra", "saue", "kohila", "sindi", "tamsalu", "kunda", "loksa", "mustvee",
	"põltsamaa", "räpina", "antsla", "tõrva", "lihula", "muhu", "lõuna-eesti", "lääne-eesti", "ida-eesti", "kesk-eesti",
	"ida-viru", "lääne-viru", "soome", "rootsi", "norra", "taani", "läti", "leedu", "poolas", "saksamaa", "prantsusmaa",
	"inglismaa", "hispaania", "itaalia", "kreeka", "türgi", "venemaa", "ukraina", "ameerika", "egiptus", "islandi",
	"austria", "šveits", "portugal", "horvaatia", "tampere", "oulu", "kuopio", "espoo", "vantaa", "yorgi",
	"riia", "riiga", "helsingi", "stockholm", "oslo", "kopenhaag", "vilnius", "varssavi", "berliin",
	"pariis", "london", "rooma", "madrid", "moskva", "peterburi", "praha", "viinis", "brüssel", "amsterdam", "budapest",
	"ateena", "lissabon", "dublin", "tokio", "peking"}

// placeToday is today's date where p is (Open-Meteo's timezone=auto dates its days there), else in zone (nil:
// time.Local).
func placeToday(p Place, zone *time.Location, now time.Time) string {
	if zone == nil {
		zone = time.Local
	}
	if loc, err := time.LoadLocation(p.Timezone); err == nil && p.Timezone != "" {
		zone = loc
	}
	return now.In(zone).Format("2006-01-02")
}

// fromToday is f with its days before today dropped (a forecast memoised before midnight starts at yesterday, N12);
// false when today isn't among its days.
func fromToday(f Forecast, today string) (Forecast, bool) {
	for i, t := range f.Daily.Time {
		if t == today {
			y := &f.Daily
			y.Time, y.Code, y.Max, y.Min = tail(y.Time, i), tail(y.Code, i), tail(y.Max, i), tail(y.Min, i)
			y.Precip, y.PrecipP = tail(y.Precip, i), tail(y.PrecipP, i)
			return f, true
		}
	}
	return f, false
}

func tail[T any](s []T, i int) []T {
	if i > len(s) {
		return nil
	}
	return s[i:]
}

// replyDay is the forecast day the ask names (0 today, 1 homme, 2 ülehomme); false for an ask the template can't
// answer: another day (weekday, week, date, "eile", "pärast", "järgmisel"), wind or sunshine.
func replyDay(toks []string) (int, bool) {
	d := 0
	for _, w := range toks {
		switch {
		case isNumeral(w) || asks(etUnanswered, func(p string) bool { return strings.HasPrefix(w, p) }):
			return 0, false
		case strings.HasPrefix(w, "ülehom"):
			return 2, true
		case strings.HasPrefix(w, "homme") || strings.HasPrefix(w, "homne") || strings.HasPrefix(w, "homs"):
			d = 1
		case strings.HasPrefix(w, "nädal") || asks(etWeekdays, func(p string) bool { return strings.HasPrefix(w, p) }):
			return 0, false
		}
	}
	return d, true
}

var etUnanswered = []string{"tuul", "päike", "eile", "pärast", "järgmis", "möödunud", "jaanuar", "veebruar", "märts",
	"aprill", "juuni", "juuli", "august", "septemb", "oktoob", "novemb", "detsemb"}

var etWeekdays = []string{"esmaspäev", "teisipäev", "kolmapäev", "neljapäev", "reede", "laupäev", "pühapäev"}

var etDays = []string{"täna", "homme", "ülehomme"}

// speakWeather is day d's answer: a yes/no for a snow / rain ask ("võib" under a 50% chance), then its range, condition
// and precipitation chance. "vihma või lund" asks about any fall.
func speakWeather(toks []string, d int, name, in string, f Forecast) string {
	y := f.Daily
	code := y.Code[d]
	day := etDays[d]
	sure := d >= len(y.PrecipP) || y.PrecipP[d] >= 50
	yes := func(will, may string) string {
		if sure {
			return "Jah, " + day + " " + will
		}
		return capital(day) + " võib " + may
	}
	snow, rain := asks(toks, isSnowWord), asks(toks, func(w string) bool { return strings.HasPrefix(w, "vihm") })
	var b strings.Builder
	switch {
	case snow && !rain:
		b.WriteString(yesNo(isSnow(code), yes("tuleb lund. ", "tulla lund. "), "Ei, "+day+" lund ei tule. "))
	case rain && !snow:
		b.WriteString(yesNo(isRain(code), yes("tuleb vihma. ", "tulla vihma. "), "Ei, "+day+" vihma ei tule. "))
	case snow || rain || asks(toks, isFallWord):
		b.WriteString(yesNo(isSnow(code) || isRain(code), yes("sajab. ", "sadada. "), "Ei, "+day+" ei saja. "))
	}
	// The place once, after any yes/no: "Praegu on Tallinnas …" / "Homme on Tallinnas …", or "Tallinn, homme on …".
	head := day + " on " + in
	if in == "" {
		head = name + ", " + day + " on"
	}
	if d == 0 {
		c := f.Current
		b.WriteString(capital(strings.Replace(head, day, "praegu", 1)) + " " + etTemps(deg(c.Temp), deg(c.Temp)) + etCond(c.Code) + ". ")
		head = day + " on"
	}
	b.WriteString(capital(head) + " " + etTemps(deg(y.Min[d]), deg(y.Max[d])) + etCond(code))
	if d < len(y.PrecipP) {
		b.WriteString(", sademete tõenäosus " + etNum(y.PrecipP[d]) + " protsenti")
	}
	b.WriteString(".")
	return b.String()
}

func capital(s string) string {
	r := []rune(s)
	return strings.ToUpper(string(r[:1])) + string(r[1:])
}

func yesNo(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func asks(toks []string, is func(string) bool) bool {
	for _, w := range toks {
		if is(w) {
			return true
		}
	}
	return false
}

func isSnowWord(w string) bool {
	return w == "lund" || strings.HasPrefix(w, "lumi") || strings.HasPrefix(w, "lume")
}

func isFallWord(w string) bool {
	return strings.HasPrefix(w, "sajab") || strings.HasPrefix(w, "sadem") || strings.HasPrefix(w, "sajun") || w == "sadu"
}

func isSnow(code int) bool { return code >= 71 && code <= 77 || code == 85 || code == 86 }

func isRain(code int) bool { return code >= 51 && code <= 67 || code >= 80 && code <= 82 || code >= 95 }

// etTemps speaks lo..hi °C: "kaks kuni kuus kraadi sooja", "kolm kraadi külma", "miinus üks kuni pluss kaks kraadi".
func etTemps(lo, hi int) string {
	span := func(a, b int) string {
		if a == b {
			return etNum(a)
		}
		return etNum(a) + " kuni " + etNum(b)
	}
	switch {
	case lo == 0 && hi == 0:
		return "null kraadi"
	case lo >= 0:
		return span(lo, hi) + " kraadi sooja"
	case hi <= 0:
		return span(-hi, -lo) + " kraadi külma"
	}
	return "miinus " + etNum(-lo) + " kuni pluss " + etNum(hi) + " kraadi"
}

var etOnes = []string{"null", "üks", "kaks", "kolm", "neli", "viis", "kuus", "seitse", "kaheksa", "üheksa"}

// etNum speaks 0..100 in words (the et {speech} block: numbers that aren't times or dates as words).
func etNum(n int) string {
	switch {
	case n < 0:
		return "miinus " + etNum(-n)
	case n < 10:
		return etOnes[n]
	case n == 10:
		return "kümme"
	case n < 20:
		return etOnes[n-10] + "teist"
	case n < 100:
		s := etOnes[n/10] + "kümmend"
		if n%10 != 0 {
			s += " " + etOnes[n%10]
		}
		return s
	case n == 100:
		return "sada"
	}
	return strconv.Itoa(n)
}

// etCond is ", <condition>" for a WMO code, "" for an unknown one.
func etCond(code int) string {
	s := map[int]string{0: "selge", 1: "peamiselt selge", 2: "vahelduv pilvisus", 3: "pilves", 45: "udu", 48: "udu",
		51: "uduvihm", 53: "uduvihm", 55: "uduvihm", 56: "jäätuv uduvihm", 57: "jäätuv uduvihm", 61: "nõrk vihm",
		63: "vihm", 65: "tugev vihm", 66: "jäävihm", 67: "jäävihm", 71: "nõrk lumesadu", 73: "lumesadu",
		75: "tugev lumesadu", 77: "lumeterad", 80: "hoovihm", 81: "hoovihm", 82: "tugev hoovihm", 85: "lumehood",
		86: "lumehood", 95: "äike", 96: "äike ja rahe", 99: "äike ja rahe"}[code]
	if s == "" {
		return ""
	}
	return ", " + s
}
