package wirepod_ttr

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
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
	forkWeatherAPISearchURL = "http://api.weatherapi.com/v1/search.json"
	forkOWMGeoURL           = "http://api.openweathermap.org/geo/1.0/direct"
	forkProbeClient         = &http.Client{Timeout: 3 * time.Second}
)

// forkLocationExists asks the weather provider whether a result is named loc (search.json autocompletes); a var so tests stub it.
var forkLocationExists = func(loc string) bool {
	w := vars.APIConfig.Weather
	if !w.Enable || w.Key == "" {
		return false
	}
	var u string
	switch w.Provider {
	case "weatherapi.com":
		u = forkWeatherAPISearchURL + "?key=" + url.QueryEscape(w.Key) + "&q=" + url.QueryEscape(loc)
	case "openweathermap.org":
		u = forkOWMGeoURL + "?q=" + url.QueryEscape(loc) + "&limit=1&appid=" + url.QueryEscape(w.Key)
	default:
		return false
	}
	resp, err := forkProbeClient.Get(u)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // url.Error's text carries the key=/appid= URL
		}
		logger.Println("Weather location probe failed:", err)
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
