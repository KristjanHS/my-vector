package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Open-Meteo endpoints (no key); vars so tests point them at httptest.
var (
	GeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
	ForecastURL = "https://api.open-meteo.com/v1/forecast"
)

// Place is a geocoded weather place.
type Place struct {
	Name     string  `json:"name"`
	Lat      float64 `json:"latitude"`
	Lon      float64 `json:"longitude"`
	Country  string  `json:"country"`
	Timezone string  `json:"timezone"`
}

// Forecast is Open-Meteo's current + daily reply (forecast_days=3, timezone=auto).
type Forecast struct {
	Current struct {
		Temp  float64 `json:"temperature_2m"`
		Code  int     `json:"weather_code"`
		Wind  float64 `json:"wind_speed_10m"`
		IsDay int     `json:"is_day"`
	} `json:"current"`
	Daily struct {
		Time    []string  `json:"time"`
		Code    []int     `json:"weather_code"`
		Max     []float64 `json:"temperature_2m_max"`
		Min     []float64 `json:"temperature_2m_min"`
		Precip  []float64 `json:"precipitation_sum"`
		PrecipP []int     `json:"precipitation_probability_max"`
	} `json:"daily"`
}

func getJSON(ctx context.Context, base string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", base, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Geocode resolves place to its best Open-Meteo match; no match is an error.
func Geocode(ctx context.Context, place string) (Place, error) {
	var r struct{ Results []Place }
	q := url.Values{"name": {place}, "count": {"1"}, "language": {"en"}, "format": {"json"}}
	if err := getJSON(ctx, GeocodeURL, q, &r); err != nil {
		return Place{}, err
	}
	if len(r.Results) == 0 {
		return Place{}, errors.New("geocode " + place + ": no match")
	}
	return r.Results[0], nil
}

// FetchForecast fetches the current conditions and a 3-day daily forecast at lat, lon.
func FetchForecast(ctx context.Context, lat, lon float64) (Forecast, error) {
	var f Forecast
	ff := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	q := url.Values{
		"latitude": {ff(lat)}, "longitude": {ff(lon)},
		"current":       {"temperature_2m,weather_code,wind_speed_10m,is_day"},
		"daily":         {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,precipitation_probability_max"},
		"forecast_days": {"3"}, "timezone": {"auto"},
	}
	if err := getJSON(ctx, ForecastURL, q, &f); err != nil {
		return Forecast{}, err
	}
	d := f.Daily
	if n := len(d.Time); n == 0 || len(d.Code) < n || len(d.Max) < n || len(d.Min) < n || len(d.Precip) < n {
		return Forecast{}, errors.New("forecast: short daily arrays")
	}
	return f, nil
}

// wmo names a WMO weather code (Open-Meteo weather_code).
func wmo(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1:
		return "mainly clear"
	case 2:
		return "partly cloudy"
	case 3:
		return "overcast"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61:
		return "light rain"
	case 63:
		return "rain"
	case 65:
		return "heavy rain"
	case 66, 67:
		return "freezing rain"
	case 71:
		return "light snow"
	case 73:
		return "snow"
	case 75:
		return "heavy snow"
	case 77:
		return "snow grains"
	case 80, 81:
		return "rain showers"
	case 82:
		return "heavy rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	}
	return "weather code " + strconv.Itoa(code)
}

// deg rounds °C to an int, so -0.4 reads 0, not -0.
func deg(v float64) int { return int(math.Round(v)) }

// WeatherBlock is the English {search} block for f at place: now, then today, tomorrow and the third day.
func WeatherBlock(place string, f Forecast) string {
	c := f.Current
	var b strings.Builder
	fmt.Fprintf(&b, "Live weather in %s (Open-Meteo):\nNow: %d°C, %s, wind %.0f km/h.", place, deg(c.Temp), wmo(c.Code), c.Wind)
	d := f.Daily
	for i, day := range d.Time {
		label := day
		switch i {
		case 0:
			label = "Today"
		case 1:
			label = "Tomorrow"
		default:
			if t, err := time.Parse(time.DateOnly, day); err == nil {
				label = t.Weekday().String()
			}
		}
		fmt.Fprintf(&b, "\n%s: %d to %d°C, %s, precipitation %.1f mm", label, deg(d.Min[i]), deg(d.Max[i]), wmo(d.Code[i]), d.Precip[i])
		if i < len(d.PrecipP) {
			fmt.Fprintf(&b, " (%d%% chance)", d.PrecipP[i])
		}
		b.WriteString(".")
	}
	return b.String()
}
