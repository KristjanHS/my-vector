package search

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// useConfig points lang.Path (so StatePath: fresh quota, cache and geocodes) at a temp myvectorConfig.json holding body.
func useConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := lang.Path
	lang.Path = func() string { return path }
	t.Cleanup(func() { lang.Path = old })
}

// fakeOpenMeteo serves geoTallinn at GeocodeURL and forecastJS at ForecastURL, recording each query.
func fakeOpenMeteo(t *testing.T) *[]string {
	t.Helper()
	var qs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		qs = append(qs, r.URL.RawQuery)
		if r.URL.Path == "/geo" {
			w.Write([]byte(geoTallinn))
			return
		}
		w.Write([]byte(forecastJS))
	}))
	t.Cleanup(srv.Close)
	g, f := GeocodeURL, ForecastURL
	GeocodeURL, ForecastURL = srv.URL+"/geo", srv.URL+"/forecast"
	t.Cleanup(func() { GeocodeURL, ForecastURL = g, f })
	return &qs
}

func TestZoneCity(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	tln, _ := time.LoadLocation("Europe/Tallinn")
	for loc, want := range map[*time.Location]string{tln: "Tallinn", ny: "New York", time.UTC: "", time.Local: "", nil: ""} {
		if got := ZoneCity(loc); got != want {
			t.Errorf("ZoneCity(%v) = %q, want %q", loc, got, want)
		}
	}
}

func TestFillWeather(t *testing.T) {
	tln, _ := time.LoadLocation("Europe/Tallinn")
	qs := fakeOpenMeteo(t)

	useConfig(t, `{}`)
	if got := Fill("mis ilm homme on?", lang.Estonian, prompt.Ollama, tln); !strings.HasPrefix(got, "Live weather in Tallinn (Open-Meteo):\nNow: 0°C") {
		t.Errorf("zone city: Fill = %q", got)
	}
	if len(*qs) != 2 || !strings.Contains((*qs)[0], "name=Tallinn") {
		t.Fatalf("queries %q, want geocode Tallinn + forecast", *qs)
	}
	Fill("what's the weather tomorrow?", lang.English, prompt.Claude, tln)
	if len(*qs) != 3 {
		t.Errorf("%d calls, want 3 (geocode cached on the second Fill)", len(*qs))
	}

	useConfig(t, `{"search": {"weather_place": "Tartu"}}`)
	*qs = nil
	Fill("kas homme sajab?", lang.Estonian, prompt.Ollama, tln)
	if len(*qs) == 0 || !strings.Contains((*qs)[0], "name=Tartu") {
		t.Errorf("configured place: queries %q", *qs)
	}
	useConfig(t, `{"search": {"weather_place": " Tartu, Estonia"}}`)
	*qs = nil
	Fill("kas homme sajab?", lang.Estonian, prompt.Ollama, tln)
	if len(*qs) == 0 || !slices.Contains(strings.Split((*qs)[0], "&"), "name=Tartu") {
		t.Errorf("comma-qualified place: queries %q, want name=Tartu", *qs)
	}

	*qs = nil
	if got := Fill("räägi nali", lang.Estonian, prompt.Ollama, tln); got != "" || len(*qs) != 0 {
		t.Errorf("class None: Fill = %q after %d calls", got, len(*qs))
	}
	useConfig(t, `{}`)
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, time.UTC); got != Unavailable(lang.Estonian) || len(*qs) != 0 {
		t.Errorf("no place: Fill = %q after %d calls", got, len(*qs))
	}

	lines := captureLog(t)
	fakeMeteo(t, 500, geoTallinn)
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln); got != Unavailable(lang.Estonian) {
		t.Errorf("geocode 500: Fill = %q", got)
	}
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo err \d+ .*500`).MatchString((*lines)[0]) {
		t.Errorf("geocode 500: log %q, want one status line carrying the error", *lines)
	}
	*lines = nil
	fakeOpenMeteo(t)
	Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln)
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo ok \d+$`).MatchString((*lines)[0]) {
		t.Errorf("ok: log %q, want one status line", *lines)
	}
	*lines = nil

	useConfig(t, `{}`) // fresh state: no cached block or geocode
	old := Timeout
	Timeout = time.Millisecond
	t.Cleanup(func() { Timeout = old })
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		if r.URL.Path == "/geo" {
			w.Write([]byte(geoTallinn))
			return
		}
		w.Write([]byte(forecastJS))
	}))
	t.Cleanup(slow.Close)
	GeocodeURL, ForecastURL = slow.URL+"/geo", slow.URL+"/forecast"
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln); got != Unavailable(lang.Estonian) {
		t.Errorf("past Timeout: Fill = %q", got)
	}
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo timeout \d+$`).MatchString((*lines)[0]) {
		t.Errorf("past Timeout: log %q, want one status line", *lines)
	}
}

// fakeSearch serves body with status at TavilyURL (tavily) or as a SearXNG server, counting requests.
func fakeSearch(t *testing.T, status int, body string, delay time.Duration) (string, *int) {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		time.Sleep(delay)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

// chainConfig points Tavily at tavily and SearXNG at searxng, with provider as the selection.
func chainConfig(t *testing.T, provider, tavily, searxng string) {
	t.Helper()
	old := TavilyURL
	TavilyURL = tavily
	t.Cleanup(func() { TavilyURL = old })
	useConfig(t, fmt.Sprintf(`{"search": {"provider": %q, "tavily_key": "K", "searxng_url": %q}}`, provider, searxng))
}

func TestFillChain(t *testing.T) {
	const live = "mis on euro kurss?"
	now := time.Now()
	okT, nT := fakeSearch(t, 200, providerFixture, 0)
	okS, nS := fakeSearch(t, 200, providerFixture, 0)
	bad, nBad := fakeSearch(t, 500, `{}`, 0)
	empty, nEmpty := fakeSearch(t, 200, `{"results":[]}`, 0)
	reset := func() { *nT, *nS, *nBad, *nEmpty = 0, 0, 0, 0 }
	results := func(got string) bool {
		return strings.HasPrefix(got, "Live web results") && strings.Contains(got, "Tartu ilm")
	}

	chainConfig(t, "auto", okT, okS)
	lines := captureLog(t)
	if got := Fill(live, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT != 1 || *nS != 0 {
		t.Errorf("auto: Fill = %q, tavily %d searxng %d calls; want tavily only", got, *nT, *nS)
	}
	used := current().UsedThisMonth("tavily", now)
	reset()
	if got := Fill(live, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT+*nS != 0 || current().UsedThisMonth("tavily", now) != used {
		t.Errorf("cache hit: Fill = %q after %d calls, used %d → %d", got, *nT+*nS, used, current().UsedThisMonth("tavily", now))
	}
	if want := `^myvector: search live cache ok \d+$`; len(*lines) != 2 || !regexp.MustCompile(want).MatchString((*lines)[1]) {
		t.Errorf("log %q, want tavily ok then a cache line", *lines)
	}

	chainConfig(t, "auto", okT, okS)
	st := current()
	st.Month, st.Used["tavily"] = month(now), CapTavily
	reset()
	*lines = nil
	if got := Fill(live, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT != 0 || *nS != 1 {
		t.Errorf("tavily capped: Fill = %q, tavily %d searxng %d calls", got, *nT, *nS)
	}
	if len(*lines) != 2 || !regexp.MustCompile(`^myvector: search live tavily capped \d+$`).MatchString((*lines)[0]) {
		t.Errorf("tavily capped: log %q", *lines)
	}

	for name, first := range map[string]string{"500": bad, "no results": empty} {
		chainConfig(t, "auto", first, okS)
		reset()
		*lines = nil
		if got := Fill(live, lang.Estonian, prompt.Ollama, nil); !results(got) || *nS != 1 || *nBad+*nEmpty != 1 {
			t.Errorf("tavily %s: Fill = %q, searxng %d calls", name, got, *nS)
		}
		if len(*lines) != 2 || !regexp.MustCompile(`^myvector: search live tavily err \d+ .*`+name).MatchString((*lines)[0]) {
			t.Errorf("tavily %s: log %q", name, *lines)
		}
	}

	chainConfig(t, "searxng", okT, okS)
	reset()
	if got := Fill(live, lang.Estonian, prompt.Claude, nil); !results(got) || *nS != 1 || *nT != 0 {
		t.Errorf("searxng selected: Fill = %q, tavily %d searxng %d calls; want searxng first", got, *nT, *nS)
	}

	chainConfig(t, "auto", bad, empty)
	if got := Fill("latest news", lang.English, prompt.Ollama, nil); got != Unavailable(lang.English) {
		t.Errorf("all fail: Fill = %q", got)
	}
	reset()
	if got := Fill("latest news", lang.English, prompt.Ollama, nil); got != Unavailable(lang.English) || *nBad != 1 {
		t.Errorf("failure cached: Fill = %q after %d calls", got, *nBad)
	}

	old := Timeout
	Timeout = 20 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	slow, _ := fakeSearch(t, 200, providerFixture, 100*time.Millisecond)
	chainConfig(t, "auto", slow, okS)
	reset()
	*lines = nil
	if got := Fill(live, lang.Estonian, prompt.Ollama, nil); got != Unavailable(lang.Estonian) || *nS != 0 {
		t.Errorf("past Timeout: Fill = %q, searxng %d calls", got, *nS)
	}
	if len(*lines) != 2 || !strings.Contains((*lines)[0], "tavily timeout") || !strings.Contains((*lines)[1], "searxng timeout") {
		t.Errorf("past Timeout: log %q, want tavily then searxng timeout", *lines)
	}
	Timeout = old

	chainConfig(t, "off", okT, okS)
	reset()
	if got := Fill(live, lang.Estonian, prompt.Ollama, nil); got != "" || *nT+*nS != 0 {
		t.Errorf("off: Fill = %q after %d calls", got, *nT+*nS)
	}
	if got := Fill("räägi nali", lang.Estonian, prompt.Ollama, nil); got != "" || *nT+*nS != 0 {
		t.Errorf("class None: Fill = %q after %d calls", got, *nT+*nS)
	}
	tln, _ := time.LoadLocation("Europe/Tallinn")
	fakeOpenMeteo(t)
	if got := Fill("mis ilm homme on?", lang.Estonian, prompt.Ollama, tln); !strings.HasPrefix(got, "Live weather in Tallinn") {
		t.Errorf("off keeps weather (W16): Fill = %q", got)
	}
	if u := current().UsedThisMonth("open-meteo", now); u != 2 {
		t.Errorf("open-meteo used %d, want 2 (geocode + forecast)", u)
	}
}

// captureLog swaps logf for one recording each line as fmt.Sprintln would print it.
func captureLog(t *testing.T) *[]string {
	t.Helper()
	var lines []string
	old := logf
	logf = func(a ...any) { lines = append(lines, strings.TrimSuffix(fmt.Sprintln(a...), "\n")) }
	t.Cleanup(func() { logf = old })
	return &lines
}
