package wirepod_ttr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
)

type quietStream struct {
	pb.ChipperGrpc_StreamingIntentGraphServer
	sent []string
}

func (s *quietStream) Send(r *pb.IntentGraphResponse) error {
	s.sent = append(s.sent, r.IntentResult.Action)
	return nil
}

// quietRig: temp lock file, one robot at a fake IP, a plugin that claims any text containing "shut" or "light",
// and a nap server whose hits land on the returned channel.
func quietRig(t *testing.T, ip string) (*vtt.IntentGraphRequest, *quietStream, chan string) {
	t.Setenv("MYVECTOR_QUIET_FILE", filepath.Join(t.TempDir(), "quiet-until"))
	t.Setenv("MYVECTOR_NAP", "")
	naps := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { naps <- r.URL.RawQuery }))
	t.Cleanup(srv.Close)
	oldURL, oldBots := quiet.NapURL, vars.BotInfo
	quiet.NapURL = func(string) string { return srv.URL }
	vars.BotInfo = vars.RobotInfoStore{}
	if ip != "" {
		if err := json.Unmarshal([]byte(`{"robots":[{"esn":"00e20100","ip_address":"`+ip+`"}]}`), &vars.BotInfo); err != nil {
			t.Fatal(err)
		}
	}
	oldU, oldF, oldN := PluginUtterances, PluginFunctions, PluginNames
	utt := []string{"shut", "light"}
	PluginUtterances = []*[]string{&utt}
	PluginFunctions = []func(string, string, string, string) (string, string){
		func(string, string, string, string) (string, string) { return "intent_test_plugin", "" },
	}
	PluginNames = []string{"test"}
	t.Cleanup(func() {
		forkNaps.Wait()
		quiet.NapURL, vars.BotInfo = oldURL, oldBots
		PluginUtterances, PluginFunctions, PluginNames = oldU, oldF, oldN
	})
	s := &quietStream{}
	return &vtt.IntentGraphRequest{Stream: s, Device: "00e20100"}, s, naps
}

func TestForkQuietShortCircuits(t *testing.T) {
	req, s, naps := quietRig(t, "10.0.0.9")
	before := time.Now()
	if !ProcessTextAll(req, "Vector, shut up please!", nil, true) {
		t.Fatal("ProcessTextAll = false, want the quiet guard to claim it")
	}
	if len(s.sent) != 1 || s.sent[0] != "intent_system_sleep" {
		t.Errorf("sent %v, want only intent_system_sleep (ahead of the plugin)", s.sent)
	}
	until, src, err := quiet.Until()
	if err != nil || src != "voice:shut up" || until.Before(before.Add(quiet.LockFor-time.Second)) {
		t.Errorf("lock = %v %q %v, want ~now+LockFor voice:shut up", until, src, err)
	}
	select {
	case q := <-naps:
		v, err := url.ParseQuery(q)
		sec, _ := strconv.ParseInt(v.Get("value"), 10, 64)
		if want := before.Add(quiet.NapFor).Unix(); err != nil || v.Get("key") != "SleepCycle_NapUntil_s" || sec < want-2 || sec > want+2 {
			t.Errorf("nap query %q, want SleepCycle_NapUntil_s=%d (epoch s, now+NapFor)", q, want)
		}
	case <-time.After(2 * time.Second):
		t.Error("no nap request")
	}
}

func TestForkQuietReachesPlugin(t *testing.T) {
	req, s, naps := quietRig(t, "10.0.0.9")
	if !ProcessTextAll(req, "turn off the kitchen light", nil, true) {
		t.Fatal("ProcessTextAll = false, want the plugin to claim it")
	}
	if len(s.sent) != 1 || s.sent[0] != "intent_test_plugin" {
		t.Errorf("sent %v, want the plugin intent", s.sent)
	}
	if until, _, _ := quiet.Until(); !until.IsZero() {
		t.Errorf("lock set to %v on a non-phrase", until)
	}
	select {
	case q := <-naps:
		t.Errorf("nap sent on a non-phrase: %q", q)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestForkQuietNoIPSkipsNap(t *testing.T) {
	req, s, naps := quietRig(t, "")
	if !ProcessTextAll(req, "ole vait", nil, true) || len(s.sent) != 1 || s.sent[0] != "intent_system_sleep" {
		t.Fatalf("sent %v, want intent_system_sleep", s.sent)
	}
	if _, src, _ := quiet.Until(); src != "voice:ole vait" {
		t.Errorf("lock source %q, want voice:ole vait", src)
	}
	select {
	case q := <-naps:
		t.Errorf("nap sent with no robot IP: %q", q)
	case <-time.After(200 * time.Millisecond):
	}
}
