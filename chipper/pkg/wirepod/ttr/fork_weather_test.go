package wirepod_ttr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
)

// stubWeather disables the provider (getWeather echoes the location) and records location probes.
func stubWeather(t *testing.T, hits ...string) *[]string {
	oldWeather, oldExists, oldGet := vars.APIConfig.Weather, forkLocationExists, forkGetWeather
	t.Cleanup(func() { vars.APIConfig.Weather, forkLocationExists, forkGetWeather = oldWeather, oldExists, oldGet })
	vars.APIConfig.Weather.Enable = false
	forkGetWeather = func(string, string, int) (forkWeatherReply, bool) { return forkWeatherReply{}, false }
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

func TestForkLocationExistsKeyless(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	fakeWeather(t, "", 200, 200, 200)
	if got := forkWeatherLocation("mis ilm on keilas", "Home", "keilas"); got != "Keila" {
		t.Errorf("keyless probe: location %q, want Keila from Open-Meteo geocode", got)
	}
}

func TestForkWMOClad(t *testing.T) {
	cases := []struct {
		code int
		day  bool
		want string
	}{{61, true, "Rain"}, {81, true, "Rain"}, {71, true, "Snow"}, {85, true, "Snow"}, {95, true, "Thunderstorms"},
		{3, true, "Cloudy"}, {45, true, "Cloudy"}, {0, true, "Sunny"}, {0, false, "Stars"}, {1, false, "Stars"}}
	for _, c := range cases {
		if got := forkWMOClad(c.code, c.day); got != c.want {
			t.Errorf("WMO %d day=%v: %q, want %q", c.code, c.day, got, c.want)
		}
	}
}

// fakeWeather serves weatherapi forecast.json and Open-Meteo geocode/forecast with the given statuses; wait() returns the weatherapi call count once the race's loser is done.
func fakeWeather(t *testing.T, key string, waStatus, geoStatus, fcStatus int) (wait func() int32) {
	var waCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wa":
			atomic.AddInt32(&waCalls, 1)
			w.WriteHeader(waStatus)
			w.Write([]byte(`{"location":{"name":"Tallinn"},"current":{"last_updated":"2026-11-07 17:00","temp_c":4.4,"temp_f":39.9,"condition":{"text":"Sunny"}},` +
				`"forecast":{"forecastday":[{"date":"2026-11-07","day":{"maxtemp_c":5,"maxtemp_f":41,"condition":{"text":"Sunny"}}},` +
				`{"date":"2026-11-08","day":{"maxtemp_c":8.6,"maxtemp_f":47.5,"condition":{"text":"light rain"}}},` +
				`{"date":"2026-11-09","day":{"maxtemp_c":1,"maxtemp_f":33.8,"condition":{"text":"Light Snow"}}}]}}`))
		case "/geo":
			w.WriteHeader(geoStatus)
			w.Write([]byte(`{"results":[{"name":"Keila","latitude":59.3,"longitude":24.4}]}`))
		case "/fc":
			w.WriteHeader(fcStatus)
			w.Write([]byte(`{"current":{"temperature_2m":-2.4,"weather_code":0,"is_day":0},"daily":{"time":["2026-11-07","2026-11-08","2026-11-09"],` +
				`"weather_code":[3,95,71],"temperature_2m_max":[1,10.4,-1],"temperature_2m_min":[0,0,0],"precipitation_sum":[0,0,0]}}`))
		}
	}))
	t.Cleanup(srv.Close)
	oldW, oldWA, oldGeo, oldFc, oldMap, oldGo := vars.APIConfig.Weather, forkWeatherAPIForecastURL, search.GeocodeURL, search.ForecastURL, forkWeatherMapPath, forkWeatherGo
	t.Cleanup(func() {
		vars.APIConfig.Weather, forkWeatherAPIForecastURL, search.GeocodeURL, search.ForecastURL, forkWeatherMapPath, forkWeatherGo = oldW, oldWA, oldGeo, oldFc, oldMap, oldGo
	})
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait) // runs first: no goroutine reads the URLs while they are restored
	forkWeatherGo = func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	vars.APIConfig.Weather.Enable, vars.APIConfig.Weather.Key, vars.APIConfig.Weather.Provider = key != "", key, "weatherapi.com"
	forkWeatherAPIForecastURL, search.GeocodeURL, search.ForecastURL = srv.URL+"/wa", srv.URL+"/geo", srv.URL+"/fc"
	forkWeatherMapPath = "../../../weather-map.json"
	return func() int32 { wg.Wait(); return atomic.LoadInt32(&waCalls) }
}

func TestForkGetWeather(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	cases := []struct {
		name, key, speech, units    string
		wa, geo, fc                 int
		cond, at, place, temp, unit string
		waCalls                     int32 // -1: weatherapi lost the race, cancelled before or after its request
	}{
		{"keyless now °C", "", "mis ilm on?", "C", 200, 200, 200, "Stars", "", "Keila", "-2", "C", 0},
		{"keyless now °F", "", "mis ilm on?", "F", 200, 200, 200, "Stars", "", "Keila", "28", "F", 0},
		{"keyless homme", "", "mis ilm homme on?", "C", 200, 200, 200, "Thunderstorms", "2026-11-08 12:00", "Keila", "10", "C", 0},
		{"keyless ülehomme", "", "kas ülehomme sajab?", "C", 200, 200, 200, "Snow", "2026-11-09 12:00", "Keila", "-1", "C", 0},
		{"open-meteo down → weatherapi homme", "K", "mis ilm homme on?", "C", 200, 500, 200, "Rain", "2026-11-08 12:00", "Tallinn", "9", "C", 1},
		{"open-meteo down → weatherapi now °F", "K", "mis ilm on?", "F", 200, 200, 500, "Sunny", "2026-11-07 17:00", "Tallinn", "40", "F", 1},
		{"weatherapi down → open-meteo homme", "K", "mis ilm homme on?", "C", 500, 200, 200, "Thunderstorms", "2026-11-08 12:00", "Keila", "10", "C", -1},
		{"both down", "K", "mis ilm homme on?", "C", 500, 500, 500, "", "test", "Home", "", "", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wait := fakeWeather(t, c.key, c.wa, c.geo, c.fc)
			cond, _, at, place, temp, unit := weatherParser(c.speech, "Home", c.units)
			if c.at == "" { // Open-Meteo "now" is the server clock
				at = ""
			}
			if c.at == "test" {
				cond, temp, unit = "", "", ""
			}
			got := strings.Join([]string{cond, at, place, temp, unit}, "|")
			want := strings.Join([]string{c.cond, c.at, c.place, c.temp, c.unit}, "|")
			if n := wait(); got != want || (c.waCalls >= 0 && n != c.waCalls) {
				t.Errorf("%s, weatherapi calls %d; want %s, %d", got, n, want, c.waCalls)
			}
		})
	}
}

func TestForkGetWeatherLeavesOWM(t *testing.T) {
	fakeGeo(t, "openweathermap.org", "[]")
	if _, ok := forkGetWeather("Tallinn", "C", 0); ok {
		t.Error("openweathermap.org with a key: forkGetWeather took it, want upstream's branch")
	}
}
