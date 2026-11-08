package wirepod_ttr

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
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
// a nap server whose hits land on the returned channel, and quietFakes on the volume/timer seam.
func quietRig(t *testing.T, ip string) (*vtt.IntentGraphRequest, *quietStream, chan string) {
	req, s, naps, _ := quietRigFake(t, ip)
	return req, s, naps
}

// quietFakes stand in for the robot volume and the restore timer; fire runs the last armed callback.
type quietFakes struct {
	mu     sync.Mutex
	vol    int
	getErr error
	gets   int
	sets   []string
	armed  []time.Duration
	fire   func()
	stops  int
	now    time.Time
}

func (f *quietFakes) setLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sets...)
}

func quietRigFake(t *testing.T, ip string) (*vtt.IntentGraphRequest, *quietStream, chan string, *quietFakes) {
	f := &quietFakes{vol: 4, now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	oldGet, oldSet, oldNow, oldAfter := forkVolumeGet, forkVolumeSet, forkQuietNow, forkQuietAfter
	forkVolumeGet = func(string) (int, error) { f.mu.Lock(); defer f.mu.Unlock(); f.gets++; return f.vol, f.getErr }
	forkVolumeSet = func(esn string, v int) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sets = append(f.sets, esn+"="+strconv.Itoa(v))
		return nil
	}
	forkQuietNow = func() time.Time { return f.now }
	forkQuietAfter = func(d time.Duration, fn func()) func() bool {
		f.armed, f.fire = append(f.armed, d), fn
		return func() bool { f.stops++; return true }
	}
	t.Cleanup(func() {
		forkNaps.Wait()
		forkVolumeGet, forkVolumeSet, forkQuietNow, forkQuietAfter, forkQuietStop = oldGet, oldSet, oldNow, oldAfter, nil
	})
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
	return &vtt.IntentGraphRequest{Stream: s, Device: "00e20100"}, s, naps, f
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

const quietESN = "00e20100"

func wantLock(t *testing.T, until time.Time, vol int, src string) {
	t.Helper()
	l, err := quiet.Read()
	if err != nil || !l.Until.Equal(until) || l.Vol != vol || l.Source != src {
		t.Fatalf("lock = %+v %v, want until %v vol %d %q", l, err, until, vol, src)
	}
}

func wantNoLock(t *testing.T) {
	t.Helper()
	if l, err := quiet.Read(); err != nil || !l.Until.IsZero() {
		t.Fatalf("lock = %+v %v, want the file gone", l, err)
	}
}

func wantSets(t *testing.T, f *quietFakes, want ...string) {
	t.Helper()
	if got := f.setLog(); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("volume sets %v, want %v", got, want)
	}
}

func TestQuietLockSetsVolumeOne(t *testing.T) {
	_, _, _, f := quietRigFake(t, "10.0.0.9")
	forkQuietLock(quietESN, "shut up", f.now)
	forkNaps.Wait()
	wantLock(t, f.now.Add(quiet.LockFor), 4, "voice:shut up")
	wantSets(t, f, quietESN+"=1")
	if len(f.armed) != 1 || f.armed[0] != quiet.LockFor {
		t.Fatalf("restore armed %v, want [LockFor]", f.armed)
	}
	f.vol, f.now = 1, f.now.Add(time.Minute)
	forkQuietLock(quietESN, "shut up", f.now)
	forkNaps.Wait()
	wantLock(t, f.now.Add(quiet.LockFor), 4, "voice:shut up")
	if f.gets != 1 {
		t.Errorf("volume read %d times, want 1 (an extend keeps the first saved value)", f.gets)
	}
}

func TestQuietVolumeReadFails(t *testing.T) {
	_, _, _, f := quietRigFake(t, "10.0.0.9")
	f.getErr = os.ErrDeadlineExceeded
	forkQuietLock(quietESN, "shut up", f.now)
	forkNaps.Wait()
	wantLock(t, f.now.Add(quiet.LockFor), quiet.NoVolume, "voice:shut up")
	wantSets(t, f, quietESN+"=1")
	f.now = f.now.Add(quiet.LockFor)
	f.fire()
	wantSets(t, f, quietESN+"=1")
	wantNoLock(t)
}

func TestQuietRestoreOnExpiry(t *testing.T) {
	_, _, _, f := quietRigFake(t, "10.0.0.9")
	forkQuietLock(quietESN, "shut up", f.now)
	forkNaps.Wait()
	if err := quiet.Set(f.now.Add(3*time.Hour), "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(quiet.LockFor)
	f.fire()
	if len(f.armed) != 2 || f.armed[1] != time.Hour {
		t.Fatalf("extended lock: armed %v, want a re-arm for the last 1h", f.armed)
	}
	wantLock(t, f.now.Add(time.Hour), 4, "voice:shut up")
	f.now = f.now.Add(time.Hour)
	f.fire()
	wantSets(t, f, quietESN+"=1", quietESN+"=4")
	wantNoLock(t)
}

func quietPost(t *testing.T, method, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(w, httptest.NewRequest(method, ForkQuietAPI+query, nil))
	forkNaps.Wait()
	return w
}

func TestQuietOffRestores(t *testing.T) {
	_, _, naps, f := quietRigFake(t, "10.0.0.9")
	if err := quiet.SetVol(f.now.Add(time.Hour), 4, "voice:shut up"); err != nil {
		t.Fatal(err)
	}
	ForkQuietRearm()
	if w := quietPost(t, http.MethodPost, "?off=1"); w.Code != http.StatusOK {
		t.Fatalf("off: %d %q", w.Code, w.Body.String())
	}
	wantSets(t, f, quietESN+"=4")
	wantNoLock(t)
	if f.stops != 1 {
		t.Errorf("restore timer stopped %d times, want 1", f.stops)
	}
	select {
	case q := <-naps:
		if v, _ := url.ParseQuery(q); v.Get("value") != "0" {
			t.Errorf("off nap %q, want value=0", q)
		}
	default:
		t.Error("off sent no nap")
	}
}

func TestQuietOnEndpoint(t *testing.T) {
	_, _, naps, f := quietRigFake(t, "10.0.0.9")
	for _, c := range []struct{ method, query string }{
		{http.MethodGet, "?on=5"}, {http.MethodPost, "?on=0"}, {http.MethodPost, "?on=x"},
		{http.MethodPost, ""}, {http.MethodPost, "?on=5&off=1"}, {http.MethodPost, "?off=yes"},
	} {
		if w := quietPost(t, c.method, c.query); w.Code == http.StatusOK {
			t.Errorf("%s %s = 200, want refused", c.method, c.query)
		}
	}
	wantNoLock(t)
	if w := quietPost(t, http.MethodPost, "?on=30"); w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "QUIET: ") {
		t.Fatalf("on=30: %d %q", w.Code, w.Body.String())
	}
	until := f.now.Add(30 * time.Minute)
	wantLock(t, until, 4, "cli:quiet.sh")
	wantSets(t, f, quietESN+"=1")
	if q := <-naps; !strings.Contains(q, "value="+strconv.FormatInt(until.Unix(), 10)) {
		t.Errorf("on nap %q, want value=%d", q, until.Unix())
	}
}

func TestQuietRearmAtStart(t *testing.T) {
	_, _, _, f := quietRigFake(t, "10.0.0.9")
	write := func(line string) {
		if err := os.WriteFile(quiet.Path(), []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ForkQuietRearm()
	if len(f.armed) != 0 {
		t.Fatalf("no lock file: armed %v", f.armed)
	}
	write(strconv.FormatInt(f.now.Add(90*time.Minute).Unix(), 10) + " 3 cli:quiet.sh")
	ForkQuietRearm()
	if len(f.armed) != 1 || f.armed[0] != 90*time.Minute {
		t.Fatalf("live lock: armed %v, want [90m]", f.armed)
	}
	write(strconv.FormatInt(f.now.Add(-time.Minute).Unix(), 10) + " 3 voice:shut up")
	ForkQuietRearm()
	if len(f.armed) != 2 || f.armed[1] != 0 {
		t.Fatalf("expired lock: armed %v, want an immediate restore", f.armed)
	}
	f.fire()
	wantSets(t, f, quietESN+"=3")
	wantNoLock(t)
}
