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
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, time.UTC); got != "" || len(*qs) != 0 {
		t.Errorf("no place: Fill = %q after %d calls", got, len(*qs))
	}

	lines := captureLog(t)
	fakeMeteo(t, 500, geoTallinn)
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln); got != "" {
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
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln); got != "" {
		t.Errorf("past Timeout: Fill = %q", got)
	}
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo timeout \d+$`).MatchString((*lines)[0]) {
		t.Errorf("past Timeout: log %q, want one status line", *lines)
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
