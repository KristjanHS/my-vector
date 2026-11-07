package wirepod_ttr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// forkWeatherExonyms: et-EE inessive → the name the weather provider knows (E24); no probe for these.
var forkWeatherExonyms = map[string]string{
	"tallinnas": "Tallinn", "tartus": "Tartu", "pärnus": "Pärnu", "narvas": "Narva", "viljandis": "Viljandi",
	"rakveres": "Rakvere", "kuressaares": "Kuressaare", "haapsalus": "Haapsalu", "paides": "Paide",
	"jõhvis": "Jõhvi", "võrus": "Võru", "valgas": "Valga", "eestis": "Estonia", "riias": "Riga",
	"helsingis": "Helsinki", "stockholmis": "Stockholm", "londonis": "London", "pariisis": "Paris",
	"berliinis": "Berlin", "roomas": "Rome", "vilniuses": "Vilnius",
}

// forkWeatherStopwords end in "s" but are never a city ("meie linnas", "kodus", "ülehomses").
var forkWeatherStopwords = map[string]bool{
	"mis": true, "kas": true, "kus": true, "siis": true, "kuidas": true, "miks": true, "väljas": true, "õues": true,
	"homses": true, "ülehomses": true, "tänases": true, "ilmas": true, "ilus": true, "meil": true, "kuus": true,
	"linnas": true, "kodulinnas": true, "pealinnas": true, "külas": true, "kodus": true, "majas": true, "toas": true,
	"aias": true, "rannas": true, "metsas": true, "koolis": true, "kontoris": true, "selles": true, "järgmises": true,
}

// Probe endpoints; package vars so tests point them at an httptest server.
var (
	forkWeatherAPISearchURL = "https://api.weatherapi.com/v1/search.json"
	forkOWMGeoURL           = "http://api.openweathermap.org/geo/1.0/direct"
	forkProbeClient         = &http.Client{Timeout: 3 * time.Second}
)

// forkLocationExists asks the weather provider whether a result is named loc (search.json autocompletes); a var so tests stub it.
var forkLocationExists = func(loc string) bool {
	w := vars.APIConfig.Weather
	keyed := w.Enable && w.Key != ""
	var u string
	switch {
	case keyed && w.Provider == "weatherapi.com":
		u = forkWeatherAPISearchURL + "?key=" + url.QueryEscape(w.Key) + "&q=" + url.QueryEscape(loc)
	case keyed && w.Provider == "openweathermap.org":
		u = forkOWMGeoURL + "?q=" + url.QueryEscape(loc) + "&limit=1&appid=" + url.QueryEscape(w.Key)
	default:
		ctx, cancel := context.WithTimeout(context.Background(), forkProbeClient.Timeout)
		defer cancel()
		p, err := search.Geocode(ctx, loc)
		return err == nil && strings.EqualFold(p.Name, loc)
	}
	resp, err := forkProbeClient.Get(u)
	if err != nil {
		logger.Println("Weather location probe failed:", forkHideKey(err))
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var hits []struct {
		Name string `json:"name"` // both search.json and geo/1.0/direct results carry it
	}
	if json.Unmarshal(body, &hits) != nil {
		return false
	}
	for _, h := range hits {
		if strings.EqualFold(h.Name, loc) {
			return true
		}
	}
	return false
}

// forkWeatherLocation picks the et-EE city word (exonym, then ≤2 inessive-strip guesses) or the home location.
func forkWeatherLocation(speechText, botLocation, apiLocation string) string {
	if lang.Current() != lang.Estonian {
		return apiLocation
	}
	words := strings.FieldsFunc(strings.ToLower(speechText), func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range words {
		if !strings.HasSuffix(w, "s") || len([]rune(w)) < 4 || forkWeatherStopwords[w] {
			continue
		}
		if city, ok := forkWeatherExonyms[w]; ok {
			return city
		}
		guesses := []string{strings.TrimSuffix(w, "s")}
		if g := []rune(guesses[0]); strings.ContainsRune("aeiouõäöü", g[len(g)-1]) {
			guesses = append(guesses, string(g[:len(g)-1]))
		}
		for _, g := range guesses {
			r := []rune(g)
			g = string(unicode.ToUpper(r[0])) + string(r[1:])
			if forkLocationExists(g) {
				logger.Println("Location parsed from et-EE speech: `" + g + "`")
				return g
			}
		}
		break // ≤2 probes per request (E24): only the first candidate word is guessed
	}
	return botLocation
}

// forkHideKey drops url.Error's URL, which carries the key=/appid= query.
func forkHideKey(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

var (
	forkWeatherAPIForecastURL = "https://api.weatherapi.com/v1/forecast.json"
	forkWeatherMapPath        = "./weather-map.json"
	forkWeatherTimeout        = 3 * time.Second
	forkWeatherGo             = func(f func()) { go f() } // tests wait for the race's loser
)

// forkWeatherReply is getWeather's tuple minus is_forecast (always "false", as upstream); localTime "test" = DoWeatherError.
type forkWeatherReply struct{ condition, localTime, place, temp, unit string }

// forkGetWeather races weatherapi.com (provider + key) and keyless Open-Meteo, first ok wins (W15); ok=false leaves OWM to upstream.
var forkGetWeather = func(location, botUnits string, hoursFromNow int) (forkWeatherReply, bool) {
	w := vars.APIConfig.Weather
	keyed := w.Enable && w.Key != ""
	if keyed && w.Provider == "openweathermap.org" {
		return forkWeatherReply{}, false
	}
	unit := botUnits
	if unit != "F" && unit != "C" {
		unit = w.Unit
	}
	if unit != "F" && unit != "C" {
		unit = "F"
	}
	h, _, _ := time.Now().Clock()
	day := (h + hoursFromNow) / 24
	if day > 2 {
		day = 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), forkWeatherTimeout)
	defer cancel()
	type result struct {
		src string
		r   forkWeatherReply
		err error
	}
	ch := make(chan result, 2)
	n, start := 1, time.Now()
	forkWeatherGo(func() { r, err := forkOpenMeteo(ctx, location, unit, day); ch <- result{"open-meteo", r, err} })
	if keyed && w.Provider == "weatherapi.com" {
		n++
		forkWeatherGo(func() { r, err := forkWeatherAPI(ctx, w.Key, location, unit, day); ch <- result{"weatherapi", r, err} })
	}
	for i := 0; i < n; i++ {
		x := <-ch
		ms := time.Since(start).Milliseconds()
		if x.err == nil {
			logger.Println("myvector: weather", x.src, "ok", ms, "day", day)
			return x.r, true
		}
		logger.Println("myvector: weather", x.src, "err", ms, x.err)
	}
	return forkWeatherReply{condition: "Snow", localTime: "test", place: location, temp: "120", unit: "C"}, true
}

func forkTemp(c, f float64, unit string) string {
	if unit == "C" {
		return strconv.Itoa(int(math.Round(c)))
	}
	return strconv.Itoa(int(math.Round(f)))
}

// forkWeatherAPI fetches forecast.json (days=3) and answers current (day 0) or that day's max.
func forkWeatherAPI(ctx context.Context, key, location, unit string, day int) (forkWeatherReply, error) {
	q := url.Values{"key": {key}, "q": {location}, "days": {"3"}, "aqi": {"no"}, "alerts": {"no"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forkWeatherAPIForecastURL+"?"+q.Encode(), nil)
	if err != nil {
		return forkWeatherReply{}, forkHideKey(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return forkWeatherReply{}, forkHideKey(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return forkWeatherReply{}, fmt.Errorf("weatherapi: %s", resp.Status)
	}
	var v struct {
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
		Current struct {
			LastUpdated string  `json:"last_updated"`
			TempC       float64 `json:"temp_c"`
			TempF       float64 `json:"temp_f"`
			Condition   struct {
				Text string `json:"text"`
			} `json:"condition"`
		} `json:"current"`
		Forecast struct {
			Days []struct {
				Date string `json:"date"`
				Day  struct {
					MaxC      float64 `json:"maxtemp_c"`
					MaxF      float64 `json:"maxtemp_f"`
					Condition struct {
						Text string `json:"text"`
					} `json:"condition"`
				} `json:"day"`
			} `json:"forecastday"`
		} `json:"forecast"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v); err != nil {
		return forkWeatherReply{}, fmt.Errorf("weatherapi: %w", err)
	}
	if day == 0 {
		c := v.Current
		return forkWeatherReply{forkCladType(c.Condition.Text), c.LastUpdated, v.Location.Name, forkTemp(c.TempC, c.TempF, unit), unit}, nil
	}
	if len(v.Forecast.Days) <= day {
		return forkWeatherReply{}, fmt.Errorf("weatherapi: %d forecast days, want day %d", len(v.Forecast.Days), day)
	}
	d := v.Forecast.Days[day]
	return forkWeatherReply{forkCladType(d.Day.Condition.Text), d.Date + " 12:00", v.Location.Name, forkTemp(d.Day.MaxC, d.Day.MaxF, unit), unit}, nil
}

// forkCladType maps a weatherapi condition text to the robot's animation via weather-map.json, case-folded; unmatched → the text.
func forkCladType(text string) string {
	path := forkWeatherMapPath
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		path = vars.AndroidPath + "/static/weather-map.json"
	}
	var m weatherAPICladStruct
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &m)
	for _, e := range m {
		if strings.EqualFold(e.APIValue, strings.TrimSpace(text)) {
			return e.CladType
		}
	}
	return text
}

// forkOpenMeteo geocodes the city part of location and answers current (day 0) or that day's max.
func forkOpenMeteo(ctx context.Context, location, unit string, day int) (forkWeatherReply, error) {
	city := strings.TrimSpace(strings.Split(location, ",")[0])
	p, err := search.Geocode(ctx, city)
	if err != nil {
		return forkWeatherReply{}, err
	}
	f, err := search.FetchForecast(ctx, p.Lat, p.Lon)
	if err != nil {
		return forkWeatherReply{}, err
	}
	c, code, isDay, at := f.Current.Temp, f.Current.Code, f.Current.IsDay == 1, time.Now().Format("2006-01-02 15:04")
	if day > 0 {
		if len(f.Daily.Time) <= day {
			return forkWeatherReply{}, fmt.Errorf("open-meteo: %d forecast days, want day %d", len(f.Daily.Time), day)
		}
		c, code, isDay, at = f.Daily.Max[day], f.Daily.Code[day], true, f.Daily.Time[day]+" 12:00"
	}
	return forkWeatherReply{forkWMOClad(code, isDay), at, p.Name, forkTemp(c, c*9/5+32, unit), unit}, nil
}

// forkWMOClad maps a WMO weather code to the robot's weather animation.
func forkWMOClad(code int, isDay bool) string {
	switch {
	case code >= 95:
		return "Thunderstorms"
	case code >= 71 && code <= 77 || code == 85 || code == 86:
		return "Snow"
	case code >= 51:
		return "Rain"
	case code >= 2:
		return "Cloudy" // 2, 3, fog 45/48
	case isDay:
		return "Sunny"
	}
	return "Stars"
}
