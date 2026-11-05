package wirepod_ttr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// stubWeather disables the provider (getWeather echoes the location) and records location probes.
func stubWeather(t *testing.T, hits ...string) *[]string {
	oldWeather, oldExists := vars.APIConfig.Weather, forkLocationExists
	t.Cleanup(func() { vars.APIConfig.Weather, forkLocationExists = oldWeather, oldExists })
	vars.APIConfig.Weather.Enable = false
	probes := &[]string{}
	forkLocationExists = func(loc string) bool {
		*probes = append(*probes, loc)
		for _, h := range hits {
			if h == loc {
				return true
			}
		}
		return false
	}
	return probes
}

func TestForkWeatherCityEstonian(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	cases := []struct {
		speech, want string
		hits, probes []string
	}{
		{"mis ilm on tallinnas", "Tallinn", nil, nil},
		{"mis ilm on keilas", "Keila", []string{"Keila"}, []string{"Keila"}},
		{"mis ilm on madridis", "Madrid", []string{"Madrid"}, []string{"Madridi", "Madrid"}},
		{"mis ilm on?", "Home", nil, nil},
		{"mis ilm väljas on?", "Home", nil, nil},
		{"mis ilm on kuskilas", "Home", nil, []string{"Kuskila", "Kuskil"}},
		{"mis ilm on kuskilas ja keilas", "Home", []string{"Keila"}, []string{"Kuskila", "Kuskil"}},
		{"mis ilm meie linnas on?", "Home", []string{"Linna", "Linn"}, nil},
		{"kas ülehomses ilmas kodus sajab?", "Home", []string{"Ülehomse", "Kodu"}, nil},
	}
	for _, c := range cases {
		probes := stubWeather(t, c.hits...)
		_, _, _, loc, _, _ := weatherParser(c.speech, "Home", "C")
		if loc != c.want {
			t.Errorf("%q: location %q, want %q", c.speech, loc, c.want)
		}
		if strings.Join(*probes, ",") != strings.Join(c.probes, ",") {
			t.Errorf("%q: probes %q, want %q", c.speech, *probes, c.probes)
		}
	}
}

func TestForkWeatherEnglishUnchanged(t *testing.T) {
	lang.UseForTest(t, lang.English)
	t.Setenv("STT_SERVICE", "vosk")
	probes := stubWeather(t, "london")
	_, _, _, loc, _, _ := weatherParser("what's the weather in london", "Home", "C")
	if loc != "london" || len(*probes) != 0 {
		t.Errorf("en-US: location %q, probes %q; want upstream %q, none", loc, *probes, "london")
	}
}

// fakeGeo points both probe endpoints at a server answering every query with body.
func fakeGeo(t *testing.T, provider, body string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	t.Cleanup(srv.Close)
	oldWeather, oldWA, oldOWM := vars.APIConfig.Weather, forkWeatherAPISearchURL, forkOWMGeoURL
	t.Cleanup(func() { vars.APIConfig.Weather, forkWeatherAPISearchURL, forkOWMGeoURL = oldWeather, oldWA, oldOWM })
	vars.APIConfig.Weather.Enable, vars.APIConfig.Weather.Key, vars.APIConfig.Weather.Provider = true, "SECRETKEY", provider
	forkWeatherAPISearchURL, forkOWMGeoURL = srv.URL, srv.URL
}

func TestForkLocationExistsRequiresName(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	cases := []struct {
		provider, body, want string
	}{
		{"weatherapi.com", `[{"id":1,"name":"Keila","country":"Estonia"}]`, "Keila"},
		{"weatherapi.com", `[{"id":1,"name":"Keilaküla","country":"Estonia"}]`, "Home"},
		{"openweathermap.org", `[{"name":"keila","lat":59.3,"lon":24.4}]`, "Keila"},
		{"openweathermap.org", `[{"name":"Kehila","lat":1,"lon":1}]`, "Home"},
		{"weatherapi.com", `{"error":{"code":2006,"message":"API key is invalid."}}`, "Home"},
		{"weatherapi.com", `[{"pad":"` + strings.Repeat("x", 70<<10) + `","name":"Keila"}]`, "Home"}, // past the 64 KiB cap
	}
	for _, c := range cases {
		fakeGeo(t, c.provider, c.body)
		if got := forkWeatherLocation("mis ilm on keilas", "Home", "keilas"); got != c.want {
			t.Errorf("%s %s: location %q, want %q", c.provider, c.body, got, c.want)
		}
	}
}

func TestForkLocationProbeErrorHidesKey(t *testing.T) {
	fakeGeo(t, "weatherapi.com", "[]")
	forkWeatherAPISearchURL = "http://127.0.0.1:1/v1/search.json" // nothing listens: dial error
	logger.LogTrayList = ""
	if forkLocationExists("Keila") {
		t.Fatal("probe to a closed port reported a hit")
	}
	if !strings.Contains(logger.LogTrayList, "probe failed") || strings.Contains(logger.LogTrayList, "SECRETKEY") {
		t.Errorf("probe error log %q: want the failure without the key", logger.LogTrayList)
	}
}
