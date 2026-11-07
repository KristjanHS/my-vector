package search

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
)

// Timeout bounds the whole Fill on the voice path (plan W10).
var Timeout = 2500 * time.Millisecond

var logf = logger.Println

// weatherPlace is c.WeatherPlace up to any comma (Open-Meteo geocodes a bare name: "Tartu, Estonia" →
// "Tartu"), else zone's city.
func weatherPlace(c Config, zone *time.Location) string {
	p, _, _ := strings.Cut(c.WeatherPlace, ",")
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

var errCapped = errors.New("capped")

func geocode(ctx context.Context, st *State, place string) (Place, error) {
	if p, ok := st.place(place); ok {
		return p, nil
	}
	if !st.Take("open-meteo", time.Now()) {
		return Place{}, errCapped
	}
	p, err := Geocode(ctx, place)
	if err == nil {
		st.setPlace(place, p)
	}
	return p, err
}

// CachedGeocode is Geocode through the saved places and the open-meteo quota (W5, W17).
func CachedGeocode(ctx context.Context, place string) (Place, error) {
	return geocode(ctx, current(), place)
}

// QuotaForecast is FetchForecast after an open-meteo Take (W5, W17).
func QuotaForecast(ctx context.Context, lat, lon float64) (Forecast, error) {
	return forecast(ctx, current(), lat, lon)
}

func forecast(ctx context.Context, st *State, lat, lon float64) (Forecast, error) {
	if !st.Take("open-meteo", time.Now()) {
		return Forecast{}, errCapped
	}
	return FetchForecast(ctx, lat, lon)
}

// Unavailable is the W8 {search} block when no lookup worked.
func Unavailable(l string) string {
	reply := "English"
	if l == lang.Estonian {
		reply = "Estonian"
	}
	return "Live lookup failed: no web or weather data right now. If the question needs live data, say briefly in " +
		reply + " that you can't look it up right now; never guess current facts."
}

// chain is c.Provider (when one is named) then DefaultOrder, deduplicated, keeping providers with a key / URL.
func chain(c Config) []Provider {
	var ps []Provider
	seen := map[string]bool{}
	for _, name := range append([]string{c.Provider}, DefaultOrder...) {
		if p, ok := c.ProviderFor(name); ok && !seen[name] {
			seen[name] = true
			ps = append(ps, p)
		}
	}
	return ps
}

// Fill is the {search} block for sentence: a fresh cached block, else Weather from Open-Meteo or Live / Recent
// over chain (one shared Timeout); "" for class None or Live / Recent with provider off, Unavailable when
// every lookup failed.
func Fill(sentence, l, brain string, zone *time.Location) string {
	class := Route(sentence, l)
	c := LoadConfig()
	if class == None || (class != Weather && c.Provider == "off") {
		return ""
	}
	st := current()
	start := time.Now()
	key := sentence // web blocks are sized per brain (W9); the weather block is not
	if class != Weather {
		key = brain + " " + sentence
	}
	if b, ok := st.Get(class, key, start); ok {
		logf("myvector: search", class.String(), "cache", "ok", time.Since(start).Milliseconds())
		return b
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	var block string
	if class == Weather {
		block = attempt(ctx, class, "open-meteo", func() (string, error) { return weather(ctx, st, weatherPlace(c, zone)) })
	} else {
		for _, p := range chain(c) {
			if ctx.Err() != nil {
				logf("myvector: search", class.String(), p.Name(), "timeout", time.Since(start).Milliseconds())
				break
			}
			block = attempt(ctx, class, p.Name(), func() (string, error) {
				if !st.Take(p.Name(), time.Now()) {
					return "", errCapped
				}
				rs, err := p.Search(ctx, sentence, l, SnippetsClaude)
				if err != nil {
					return "", err
				}
				if b := Block(brain, rs); b != "" {
					return b, nil
				}
				return "", errors.New("no results")
			})
			if block != "" {
				break
			}
		}
	}
	if block == "" {
		return Unavailable(l)
	}
	st.Put(class, key, block, time.Now())
	return block
}

// attempt runs one provider's lookup and logs its W14 line.
func attempt(ctx context.Context, class Class, name string, look func() (string, error)) string {
	start := time.Now()
	block, err := look()
	line := []any{"myvector: search", class.String(), name, "ok", time.Since(start).Milliseconds()}
	switch {
	case errors.Is(err, errCapped):
		line[3] = "capped"
	case errors.Is(err, context.DeadlineExceeded) || (err != nil && ctx.Err() != nil):
		line[3] = "timeout"
	case err != nil:
		line[3] = "err"
		line = append(line, err.Error())
	}
	logf(line...)
	if err != nil {
		return ""
	}
	return block
}

func weather(ctx context.Context, st *State, place string) (string, error) {
	if place == "" {
		return "", errors.New("no weather place: set search.weather_place")
	}
	p, err := geocode(ctx, st, place)
	if err != nil {
		return "", err
	}
	f, err := forecast(ctx, st, p.Lat, p.Lon)
	if err != nil {
		return "", err
	}
	return WeatherBlock(p.Name, f), nil
}
