package wirepod_ttr

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
)

// TestMain stubs forkSearch to "" and points the weather endpoints at a closed port so no ttr test reaches the
// network, and the search state (quota, geocodes) at a temp file.
func TestMain(m *testing.M) {
	forkSearch = func(sentence, l, brain string, zone *time.Location) string { return "" }
	search.GeocodeURL, search.ForecastURL, forkWeatherAPIForecastURL = "http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1"
	dir, _ := os.MkdirTemp("", "ttr-search-state")
	search.StatePath = func() string { return dir + "/myvectorSearch.json" }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestForkSearchStubbed(t *testing.T) {
	if reflect.ValueOf(forkSearch).Pointer() == reflect.ValueOf(search.Fill).Pointer() {
		t.Fatal("forkSearch is the live search.Fill: ttr tests could reach the network")
	}
	if got := forkSearch("mis ilm homme on?", "et-EE", "ollama", time.UTC); got != "" {
		t.Errorf("default forkSearch = %q, want \"\"", got)
	}
}
