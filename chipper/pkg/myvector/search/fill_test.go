package search

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	if got := Fill("mis ilm homme on?", nil, lang.Estonian, prompt.Ollama, tln); !strings.HasPrefix(got, "Live weather in Tallinn (Open-Meteo):\nNow: 0°C") {
		t.Errorf("zone city: Fill = %q", got)
	}
	if len(*qs) != 2 || !strings.Contains((*qs)[0], "name=Tallinn") {
		t.Fatalf("queries %q, want geocode Tallinn + forecast", *qs)
	}
	Fill("what's the weather tomorrow?", nil, lang.English, prompt.Claude, tln)
	if len(*qs) != 2 {
		t.Errorf("%d calls, want 2 (forecast memo serves the second Fill)", len(*qs))
	}

	useConfig(t, `{"search": {"weather_place": "Tartu"}}`)
	*qs = nil
	Fill("kas homme sajab?", nil, lang.Estonian, prompt.Ollama, tln)
	if len(*qs) == 0 || !strings.Contains((*qs)[0], "name=Tartu") {
		t.Errorf("configured place: queries %q", *qs)
	}
	useConfig(t, `{"search": {"weather_place": " Tartu, Estonia"}}`)
	*qs = nil
	Fill("kas homme sajab?", nil, lang.Estonian, prompt.Ollama, tln)
	if len(*qs) == 0 || !slices.Contains(strings.Split((*qs)[0], "&"), "name=Tartu") {
		t.Errorf("comma-qualified place: queries %q, want name=Tartu", *qs)
	}

	*qs = nil
	if got := Fill("räägi nali", nil, lang.Estonian, prompt.Ollama, tln); got != "" || len(*qs) != 0 {
		t.Errorf("class None: Fill = %q after %d calls", got, len(*qs))
	}
	useConfig(t, `{}`)
	if got := Fill("mis ilm on?", nil, lang.Estonian, prompt.Ollama, time.UTC); got != Unavailable(lang.Estonian) || len(*qs) != 0 {
		t.Errorf("no place: Fill = %q after %d calls", got, len(*qs))
	}

	lines := captureLog(t)
	fakeMeteo(t, 500, geoTallinn)
	if got := Fill("mis ilm on?", nil, lang.Estonian, prompt.Ollama, tln); got != Unavailable(lang.Estonian) {
		t.Errorf("geocode 500: Fill = %q", got)
	}
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo err \d+ .*500`).MatchString((*lines)[0]) {
		t.Errorf("geocode 500: log %q, want one status line carrying the error", *lines)
	}
	*lines = nil
	fakeOpenMeteo(t)
	Fill("mis ilm on?", nil, lang.Estonian, prompt.Ollama, tln)
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
	if got := Fill("mis ilm on?", nil, lang.Estonian, prompt.Ollama, tln); got != Unavailable(lang.Estonian) {
		t.Errorf("past Timeout: Fill = %q", got)
	}
	if len(*lines) != 1 || !regexp.MustCompile(`^myvector: search weather open-meteo timeout \d+$`).MatchString((*lines)[0]) {
		t.Errorf("past Timeout: log %q, want one status line", *lines)
	}
}

func TestQuotaWeatherHelpers(t *testing.T) {
	ctx := context.Background()
	qs := fakeOpenMeteo(t)
	useConfig(t, `{}`)
	p, err := CachedGeocode(ctx, "Tallinn")
	if _, err2 := CachedGeocode(ctx, "Tallinn"); err != nil || err2 != nil || len(*qs) != 1 {
		t.Fatalf("CachedGeocode: %d queries, err %v / %v; want one geocode", len(*qs), err, err2)
	}
	if _, err := QuotaForecast(ctx, p.Lat, p.Lon); err != nil {
		t.Fatal(err)
	}
	if u := current().UsedThisMonth("open-meteo", time.Now()); u != 2 {
		t.Errorf("open-meteo used %d, want 2 (geocode + forecast)", u)
	}
	st := current()
	st.Month, st.Used["open-meteo"] = month(time.Now()), CapOpenMeteo
	if _, err := QuotaForecast(ctx, p.Lat, p.Lon); !errors.Is(err, errCapped) || len(*qs) != 2 {
		t.Errorf("capped: err %v after %d queries, want errCapped and no call", err, len(*qs))
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
	// every result names the ask's topic, so the N6 gate drops none (and logs no relevance line)
	onTopic := strings.NewReplacer("Homme sajab.", "Euro kurss homme.", "Päike.", "Euro.", "Pilves.", "Euro.").Replace(providerFixture)
	okT, nT := fakeSearch(t, 200, onTopic, 0)
	okS, nS := fakeSearch(t, 200, onTopic, 0)
	bad, nBad := fakeSearch(t, 500, `{}`, 0)
	empty, nEmpty := fakeSearch(t, 200, `{"results":[]}`, 0)
	reset := func() { *nT, *nS, *nBad, *nEmpty = 0, 0, 0, 0 }
	results := func(got string) bool {
		return (strings.HasPrefix(got, prompt.SearchWrap(prompt.Ollama)) || strings.HasPrefix(got, prompt.SearchWrap(prompt.Claude))) && strings.Contains(got, "Tartu ilm")
	}

	chainConfig(t, "auto", okT, okS)
	lines := captureLog(t)
	if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT != 1 || *nS != 0 {
		t.Errorf("auto: Fill = %q, tavily %d searxng %d calls; want tavily only", got, *nT, *nS)
	}
	used := current().UsedThisMonth("tavily", now)
	reset()
	if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT+*nS != 0 || current().UsedThisMonth("tavily", now) != used {
		t.Errorf("cache hit: Fill = %q after %d calls, used %d → %d", got, *nT+*nS, used, current().UsedThisMonth("tavily", now))
	}
	if want := `^myvector: search live cache ok \d+$`; len(*lines) != 2 || !regexp.MustCompile(want).MatchString((*lines)[1]) {
		t.Errorf("log %q, want tavily ok then a cache line", *lines)
	}
	if got := Fill(live, nil, lang.Estonian, prompt.Claude, nil); !results(got) || *nT != 1 {
		t.Errorf("other brain: Fill after %d calls, want a fresh fetch (blocks are sized per brain)", *nT)
	}

	chainConfig(t, "auto", okT, okS)
	st := current()
	st.Month, st.Used["tavily"] = month(now), CapTavily
	reset()
	*lines = nil
	if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); !results(got) || *nT != 0 || *nS != 1 {
		t.Errorf("tavily capped: Fill = %q, tavily %d searxng %d calls", got, *nT, *nS)
	}
	if len(*lines) != 2 || !regexp.MustCompile(`^myvector: search live tavily capped \d+$`).MatchString((*lines)[0]) {
		t.Errorf("tavily capped: log %q", *lines)
	}

	for name, first := range map[string]string{"500": bad, "no results": empty} {
		chainConfig(t, "auto", first, okS)
		reset()
		*lines = nil
		if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); !results(got) || *nS != 1 || *nBad+*nEmpty != 1 {
			t.Errorf("tavily %s: Fill = %q, searxng %d calls", name, got, *nS)
		}
		if len(*lines) != 2 || !regexp.MustCompile(`^myvector: search live tavily err \d+ .*`+name).MatchString((*lines)[0]) {
			t.Errorf("tavily %s: log %q", name, *lines)
		}
		if u := current().UsedThisMonth("tavily", now); u != 1 {
			t.Errorf("tavily %s: used %d, want 1 (a failed attempt still counts)", name, u)
		}
	}

	chainConfig(t, "searxng", okT, okS)
	reset()
	if got := Fill(live, nil, lang.Estonian, prompt.Claude, nil); !results(got) || *nS != 1 || *nT != 0 {
		t.Errorf("searxng selected: Fill = %q, tavily %d searxng %d calls; want searxng first", got, *nT, *nS)
	}

	chainConfig(t, "auto", bad, empty)
	if got := Fill("latest news", nil, lang.English, prompt.Ollama, nil); got != Unavailable(lang.English) {
		t.Errorf("all fail: Fill = %q", got)
	}
	reset()
	if got := Fill("latest news", nil, lang.English, prompt.Ollama, nil); got != Unavailable(lang.English) || *nBad != 1 {
		t.Errorf("failure cached: Fill = %q after %d calls", got, *nBad)
	}

	old := Timeout
	Timeout = 20 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	slow, _ := fakeSearch(t, 200, onTopic, 100*time.Millisecond)
	chainConfig(t, "auto", slow, okS)
	reset()
	*lines = nil
	if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); got != Unavailable(lang.Estonian) || *nS != 0 {
		t.Errorf("past Timeout: Fill = %q, searxng %d calls", got, *nS)
	}
	if len(*lines) != 2 || !strings.Contains((*lines)[0], "tavily timeout") || !strings.Contains((*lines)[1], "searxng timeout") {
		t.Errorf("past Timeout: log %q, want tavily then searxng timeout", *lines)
	}
	Timeout = old

	chainConfig(t, "off", okT, okS)
	reset()
	if got := Fill(live, nil, lang.Estonian, prompt.Ollama, nil); got != "" || *nT+*nS != 0 {
		t.Errorf("off: Fill = %q after %d calls", got, *nT+*nS)
	}
	if got := Fill("räägi nali", nil, lang.Estonian, prompt.Ollama, nil); got != "" || *nT+*nS != 0 {
		t.Errorf("class None: Fill = %q after %d calls", got, *nT+*nS)
	}
	tln, _ := time.LoadLocation("Europe/Tallinn")
	fakeOpenMeteo(t)
	if got := Fill("mis ilm homme on?", nil, lang.Estonian, prompt.Ollama, tln); !strings.HasPrefix(got, "Live weather in Tallinn") {
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
	var mu sync.Mutex // race logs from two goroutines
	old := logf
	logf = func(a ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, strings.TrimSuffix(fmt.Sprintln(a...), "\n"))
	}
	t.Cleanup(func() { logf = old })
	return &lines
}

// recorder serves body after delay and records each request's Tavily body or SearXNG query.
func recorder(t *testing.T, body string, delay time.Duration) (string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.URL.RawQuery+string(b))
		mu.Unlock()
		time.Sleep(delay)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }
}

func TestFillNewsRace(t *testing.T) {
	// every snippet names the topic in both languages, so the N6 gate keeps it for each ask below
	const topic = "semikonduktorid semiconductor pooljuhid"
	const tav = `{"results":[{"title":"T1","url":"https://x.ee/1","content":"` + topic + `"},{"title":"T2","url":"https://x.ee/2","content":"` + topic + `"}]}`
	const sx = `{"results":[{"title":"S1","url":"https://y.com/1","content":"` + topic + `"},{"title":"T2 again","url":"https://x.ee/2/","content":"` + topic + `"}]}`
	tURL, tGot := recorder(t, tav, 0)
	sURL, sGot := recorder(t, sx, 0)
	chainConfig(t, "auto", tURL, sURL)
	got := Fill(turn1, nil, lang.Estonian, prompt.Claude, nil)
	i1, i2, i3 := strings.Index(got, "T1"), strings.Index(got, "S1"), strings.Index(got, "T2")
	if i1 < 0 || i2 < i1 || i3 < i2 || strings.Contains(got, "T2 again") {
		t.Errorf("merged block %q, want T1, S1, T2 interleaved and the repeated URL dropped", got)
	}
	if g := tGot(); len(g) != 1 || !strings.Contains(g[0], `"query":"semikontaktorstide finantsuudiseid`) || !strings.Contains(g[0], `"topic":"news"`) {
		t.Errorf("tavily got %q, want the et keywords in news mode", g)
	}
	if g := sGot(); len(g) != 1 || !strings.Contains(g[0], "q=semiconductor+financial+news") || !strings.Contains(g[0], "language=en") || !strings.Contains(g[0], "categories=news") {
		t.Errorf("searxng got %q, want the English keywords in news mode", g)
	}

	chainConfig(t, "auto", tURL, sURL)
	Fill("latest semiconductor news", nil, lang.English, prompt.Claude, nil)
	if g := sGot(); len(g) != 2 || !strings.Contains(g[1], "q=latest+semiconductor+news") {
		t.Errorf("en turn: searxng got %q, want the English keywords untranslated", g)
	}

	chainConfig(t, "searxng", tURL, sURL)
	Fill("otsi pooljuhtide hinnad", nil, lang.Estonian, prompt.Claude, nil)
	if g := sGot(); len(g) != 3 || !strings.Contains(g[2], "q=pooljuhtide+hinnad") || !strings.Contains(g[2], "language=et") || strings.Contains(g[2], "categories=") {
		t.Errorf("et non-news turn: searxng got %q, want the et keywords in Estonian (Q3)", g)
	}

	old := Timeout
	Timeout = 50 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	slow, _ := recorder(t, tav, 200*time.Millisecond)
	chainConfig(t, "auto", slow, sURL)
	if got := Fill(turn1, nil, lang.Estonian, prompt.Claude, nil); !strings.Contains(got, "S1") || strings.Contains(got, "T1") {
		t.Errorf("slow tavily: Fill = %q, want SearXNG's hits alone", got)
	}
}

func TestMerge(t *testing.T) {
	a := []Result{{Title: "a0", URL: "https://x.ee/n/"}, {Title: "a1"}, {Title: "a2", URL: "https://z.ee"}}
	b := []Result{{Title: "b0", URL: "https://x.ee/n"}, {Title: "b1"}, {Title: "b2", URL: "https://y.ee"}}
	var got []string
	for _, r := range merge(a, b) {
		got = append(got, r.Title)
	}
	// interleaved a0 b0 a1 b1 a2 b2; b0 is a0 with a trailing slash; a1/b1 have no URL and are both kept
	if want := []string{"a0", "a1", "b1", "a2", "b2"}; !slices.Equal(got, want) {
		t.Errorf("merge = %v, want %v", got, want)
	}
}

func TestFillFollowUp(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.URL.RawQuery+string(b))
		w.Write([]byte(providerFixture))
	}))
	t.Cleanup(srv.Close)
	chainConfig(t, "tavily", srv.URL, srv.URL) // a news ask races both providers
	const follow = "otsi viimase pakkumise pealt, number kolm."
	Fill(follow, []string{incident}, lang.Estonian, prompt.Ollama, nil)
	if len(bodies) != 2 || !strings.Contains(bodies[0], "semiconductors") || !strings.Contains(bodies[1], "semiconductor") {
		t.Fatalf("follow-up after the 08:27 ask sent %q, want one query with the topic", bodies)
	}
	Fill(follow, []string{"otsi Nvidia aktsia hinda"}, lang.Estonian, prompt.Ollama, nil)
	if len(bodies) != 4 || !strings.Contains(strings.ToLower(bodies[2]+bodies[3]), "nvidia") {
		t.Errorf("the same follow-up on another topic sent %q, want a second, uncached query", bodies)
	}
}

// TestFillRelevance: a provider whose results share no stem with the ask counts as no results (search-news-quality
// N6): the chain moves on, the news race merges only the on-topic set, and two off-topic sets are Unavailable.
func TestFillRelevance(t *testing.T) {
	const junk = `{"results":[{"title":"Räägime asjast","url":"https://youtube.com/1","content":"YouTube"},` +
		`{"title":"Uudis – Vikitsitaadid","url":"https://wikiquote.org/1","content":"Uudis on teade."},` +
		`{"title":"ERR","url":"https://err.ee","content":"Eesti Rahvusringhäälingu uudised"}]}`
	const onTopic = `{"results":[{"title":"Euro vahetuskurss","url":"https://eestipank.ee/1","content":"semiconductor stocks"}]}` // English only: SearXNG must match English(q) (N6)
	bad, nBad := fakeSearch(t, 200, junk, 0)
	good, nGood := fakeSearch(t, 200, onTopic, 0)

	chainConfig(t, "auto", bad, good)
	if got := Fill("mis on euro kurss?", nil, lang.Estonian, prompt.Ollama, nil); !strings.Contains(got, "Euro vahetuskurss") || strings.Contains(got, "Vikitsitaadid") || *nBad != 1 || *nGood != 1 {
		t.Errorf("chain: Fill = %q (%d junk, %d on-topic calls), want the next provider's hit alone", got, *nBad, *nGood)
	}

	chainConfig(t, "auto", bad, good)
	if got := Fill(turn1, nil, lang.Estonian, prompt.Ollama, nil); !strings.Contains(got, "Euro vahetuskurss") || strings.Contains(got, "ERR") {
		t.Errorf("news race: Fill = %q, want only the on-topic set merged", got)
	}

	chainConfig(t, "auto", bad, bad)
	if got := Fill("mis on euro kurss?", nil, lang.Estonian, prompt.Ollama, nil); got != Unavailable(lang.Estonian) {
		t.Errorf("both off-topic: Fill = %q, want Unavailable", got)
	}

	// Q14 + English words on an et ask: the English gold hit stays, the off-topic one goes
	mixed, _ := fakeSearch(t, 200, `{"results":[{"title":"Gold price hits record high","url":"https://a.com/1","content":"x"},`+
		`{"title":"Räägime asjast","url":"https://a.com/2","content":"YouTube"}]}`, 0)
	chainConfig(t, "tavily", mixed, bad)
	if got := Fill("mis on kulla hind?", nil, lang.Estonian, prompt.Ollama, nil); !strings.Contains(got, "Gold price") || strings.Contains(got, "Räägime") {
		t.Errorf("mixed set: Fill = %q, want the gold hit alone", got)
	}
}
