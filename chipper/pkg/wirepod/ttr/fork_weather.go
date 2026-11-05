package wirepod_ttr

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
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

// forkWeatherStopwords end in "s" but are never a city.
var forkWeatherStopwords = map[string]bool{
	"mis": true, "kas": true, "kus": true, "siis": true, "kuidas": true, "väljas": true, "õues": true,
	"homses": true, "ilmas": true, "ilus": true,
}

// forkLocationExists asks the configured weather provider whether loc resolves; a package var so tests stub it.
var forkLocationExists = func(loc string) bool {
	w := vars.APIConfig.Weather
	if !w.Enable || w.Key == "" {
		return false
	}
	var u string
	switch w.Provider {
	case "weatherapi.com":
		u = "http://api.weatherapi.com/v1/search.json?key=" + url.QueryEscape(w.Key) + "&q=" + url.QueryEscape(loc)
	case "openweathermap.org":
		u = "http://api.openweathermap.org/geo/1.0/direct?q=" + url.QueryEscape(loc) + "&limit=1&appid=" + url.QueryEscape(w.Key)
	default:
		return false
	}
	resp, err := http.Get(u)
	if err != nil {
		logger.Println("Weather location probe: " + err.Error())
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var hits []json.RawMessage
	return json.Unmarshal(body, &hits) == nil && len(hits) > 0
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
