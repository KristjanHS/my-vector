package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	geoTallinn = `{"results":[{"name":"Tallinn","latitude":59.43696,"longitude":24.75353,"country":"Estonia","timezone":"Europe/Tallinn"}]}`
	forecastJS = `{"current":{"temperature_2m":-0.4,"weather_code":3,"wind_speed_10m":12.3},
"daily":{"time":["2026-11-07","2026-11-08","2026-11-09"],"weather_code":[3,61,71],
"temperature_2m_max":[4.6,6.2,1.0],"temperature_2m_min":[-1.2,2.4,-3.5],
"precipitation_sum":[0,3.4,1.2],"precipitation_probability_max":[10,85,60]}}`
)

// fakeMeteo serves body with status at both Open-Meteo URLs and records the last query.
func fakeMeteo(t *testing.T, status int, body string) *string {
	t.Helper()
	var q string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	g, f := GeocodeURL, ForecastURL
	GeocodeURL, ForecastURL = srv.URL, srv.URL
	t.Cleanup(func() { GeocodeURL, ForecastURL = g, f })
	return &q
}

func TestWeather(t *testing.T) {
	ctx := context.Background()

	q := fakeMeteo(t, 200, geoTallinn)
	p, err := Geocode(ctx, "Tallinn")
	if err != nil || p.Name != "Tallinn" || p.Lat != 59.43696 || p.Lon != 24.75353 {
		t.Fatalf("Geocode = %+v, %v", p, err)
	}
	if !strings.Contains(*q, "name=Tallinn") {
		t.Errorf("geocode query %q lacks the place", *q)
	}

	q = fakeMeteo(t, 200, forecastJS)
	f, err := FetchForecast(ctx, p.Lat, p.Lon)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"latitude=59.4370", "forecast_days=3", "timezone=auto", "current=", "daily="} {
		if !strings.Contains(*q, want) {
			t.Errorf("forecast query %q lacks %q", *q, want)
		}
	}
	want := "Live weather in Tallinn (Open-Meteo):\n" +
		"Now: 0°C, overcast, wind 12 km/h.\n" +
		"Today: -1 to 5°C, overcast, precipitation 0.0 mm (10% chance).\n" +
		"Tomorrow: 2 to 6°C, light rain, precipitation 3.4 mm (85% chance).\n" +
		"Monday: -4 to 1°C, light snow, precipitation 1.2 mm (60% chance)."
	if got := WeatherBlock("Tallinn", f); got != want {
		t.Errorf("WeatherBlock =\n%s\nwant\n%s", got, want)
	}
	short := f
	short.Daily.Min, short.Daily.Precip = f.Daily.Min[:2], f.Daily.Precip[:1]
	if got := WeatherBlock("Tallinn", short); got != want[:strings.Index(want, "\nTomorrow")] {
		t.Errorf("WeatherBlock(unequal daily arrays) =\n%s\nwant only the days every array covers", got)
	}

	fakeMeteo(t, 200, `{}`)
	if _, err := Geocode(ctx, "Nowhere"); err == nil {
		t.Error("geocode with 0 results: want error")
	}
	if _, err := FetchForecast(ctx, 0, 0); err == nil {
		t.Error("forecast with no daily arrays: want error")
	}
	// 500 with a valid body: only the status check can reject it.
	fakeMeteo(t, 500, geoTallinn)
	if _, err := Geocode(ctx, "Tallinn"); err == nil {
		t.Error("geocode 500: want error")
	}
	fakeMeteo(t, 500, forecastJS)
	if _, err := FetchForecast(ctx, 1, 1); err == nil {
		t.Error("forecast 500: want error")
	}
}
