package search

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// useConfig points lang.Path at a temp myvectorConfig.json holding body and empties the geocode cache.
func useConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := lang.Path
	lang.Path = func() string { return path }
	geo.m = map[string]Place{}
	t.Cleanup(func() { lang.Path = old; geo.m = map[string]Place{} })
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

	*qs = nil
	if got := Fill("räägi nali", lang.Estonian, prompt.Ollama, tln); got != "" || len(*qs) != 0 {
		t.Errorf("class None: Fill = %q after %d calls", got, len(*qs))
	}
	useConfig(t, `{}`)
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, time.UTC); got != "" || len(*qs) != 0 {
		t.Errorf("no place: Fill = %q after %d calls", got, len(*qs))
	}

	fakeMeteo(t, 500, geoTallinn)
	if got := Fill("mis ilm on?", lang.Estonian, prompt.Ollama, tln); got != "" {
		t.Errorf("geocode 500: Fill = %q", got)
	}

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
}
