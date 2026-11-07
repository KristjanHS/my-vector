package search

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// Timeout bounds the whole Fill on the voice path (plan W10).
var Timeout = 2500 * time.Millisecond

var logf = logger.Println

// weatherPlace is search.weather_place in myvectorConfig.json up to any comma (Open-Meteo geocodes a bare
// name: "Tartu, Estonia" → "Tartu"), else zone's city.
func weatherPlace(zone *time.Location) string {
	p, _, _ := strings.Cut(LoadConfig().WeatherPlace, ",")
	if p = strings.TrimSpace(p); p != "" {
		return p
	}
	return ZoneCity(zone)
}

// ZoneCity is an IANA zone's city ("Europe/Tallinn" → "Tallinn", "America/New_York" → "New York"); "" for
// zones without one (UTC, Local).
func ZoneCity(zone *time.Location) string {
	if zone == nil {
		return ""
	}
	name := zone.String()
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return strings.ReplaceAll(name[i+1:], "_", " ")
}

func geocode(ctx context.Context, place string) (Place, error) {
	st := current()
	if p, ok := st.place(place); ok {
		return p, nil
	}
	p, err := Geocode(ctx, place)
	if err == nil {
		st.setPlace(place, p)
	}
	return p, err
}

// Fill is the {search} block for sentence: Weather only so far (Open-Meteo at weatherPlace); "" on no lookup,
// an error or past Timeout.
func Fill(sentence, l, brain string, zone *time.Location) string {
	class := Route(sentence, l)
	if class != Weather {
		return ""
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	block, err := weather(ctx, weatherPlace(zone))
	line := []any{"myvector: search", class.String(), "open-meteo", "ok", time.Since(start).Milliseconds()}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		line[3] = "timeout"
	case err != nil:
		line[3] = "err"
		line = append(line, err.Error())
	}
	logf(line...) // W14: exactly one line per lookup
	return block
}

func weather(ctx context.Context, place string) (string, error) {
	if place == "" {
		return "", errors.New("no weather place: set search.weather_place")
	}
	p, err := geocode(ctx, place)
	if err != nil {
		return "", err
	}
	f, err := FetchForecast(ctx, p.Lat, p.Lon)
	if err != nil {
		return "", err
	}
	return WeatherBlock(p.Name, f), nil
}
