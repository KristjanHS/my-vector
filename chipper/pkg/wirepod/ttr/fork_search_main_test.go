package wirepod_ttr

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
)

// TestMain stubs forkSearch to "" so no ttr test reaches Open-Meteo; TestForkAIReqSearch swaps in its own fake.
func TestMain(m *testing.M) {
	forkSearch = func(sentence, l, brain string, zone *time.Location) string { return "" }
	os.Exit(m.Run())
}

func TestForkSearchStubbed(t *testing.T) {
	if reflect.ValueOf(forkSearch).Pointer() == reflect.ValueOf(search.Fill).Pointer() {
		t.Fatal("forkSearch is the live search.Fill: ttr tests could reach the network")
	}
	if got := forkSearch("mis ilm homme on?", "et-EE", "ollama", time.UTC); got != "" {
		t.Errorf("default forkSearch = %q, want \"\"", got)
	}
}
