package search

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Timeout bounds the whole Fill on the voice path (plan W10).
var Timeout = 2500 * time.Millisecond

var logf = logger.Println

var geo = struct {
	sync.Mutex
	m map[string]Place
}{m: map[string]Place{}}

// weatherPlace is search.weather_place in myvectorConfig.json, else zone's city.
func weatherPlace(zone *time.Location) string {
	var c struct {
		Search struct {
			WeatherPlace string `json:"weather_place"`
		} `json:"search"`
	}
	if b, err := os.ReadFile(lang.Path()); err == nil {
		json.Unmarshal(b, &c)
	}
	if p := strings.TrimSpace(c.Search.WeatherPlace); p != "" {
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
	geo.Lock()
	p, ok := geo.m[place]
	geo.Unlock()
	if ok {
		return p, nil
	}
	p, err := Geocode(ctx, place)
	if err == nil {
		geo.Lock()
		geo.m[place] = p
		geo.Unlock()
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
	status := "ok"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		status = "timeout"
	case err != nil:
		status = "err"
		logf("myvector: search " + class.String() + ": " + err.Error())
	}
	logf("myvector: search", class.String(), "open-meteo", status, time.Since(start).Milliseconds())
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
