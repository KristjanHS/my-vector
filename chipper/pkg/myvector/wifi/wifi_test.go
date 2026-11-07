package wifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	stdin string
	args  []string
}

// fake replaces Run with one that records each call and answers out, errOut, code.
func fake(t *testing.T, out, errOut string, code int) *[]call {
	t.Helper()
	calls := &[]call{}
	old := Run
	Run = func(_ context.Context, stdin string, args ...string) (string, string, int) {
		*calls = append(*calls, call{stdin, args})
		return out, errOut, code
	}
	t.Cleanup(func() { Run = old })
	return calls
}

func do(method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, API, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	Handler(w, r)
	return w
}

const list = "connected 4c696e77696669325f326b\nsaved 4c696e77696669325f316b\nsaved 4c696e77696669325f326b\n" +
	"seen 47756573745f3247\nseen 4c696e77696669325f326b\njunk line\nseen ZZ\n"

func TestParse(t *testing.T) {
	got := Parse(list)
	want := []Network{
		{SSID: "Linwifi2_2k", Hex: "4c696e77696669325f326b", Saved: true, Seen: true, Connected: true},
		{SSID: "Linwifi2_1k", Hex: "4c696e77696669325f316b", Saved: true},
		{SSID: "Guest_2G", Hex: "47756573745f3247", Seen: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse:\n got %+v\nwant %+v", got, want)
	}
}

func TestGetLists(t *testing.T) {
	calls := fake(t, list, "", 0)
	w := do(http.MethodGet, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET: %d %s", w.Code, w.Body)
	}
	var rep reply
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil || len(rep.Networks) != 3 || !rep.Networks[0].Connected {
		t.Fatalf("GET reply %s (%v)", w.Body, err)
	}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0].args, []string{"list"}) || (*calls)[0].stdin != "" {
		t.Fatalf("calls %+v", *calls)
	}
}

func TestAddPassesPasswordOnStdinOnly(t *testing.T) {
	calls := fake(t, "ok: saved 'Linwifi2_1k'; it counts from the robot's next boot\n", "", 0)
	w := do(http.MethodPost, `{"action":"add","ssid":"Linwifi2_1k","password":"s3cret pass\"x"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add: %d %s", w.Code, w.Body)
	}
	c := (*calls)[0]
	if !reflect.DeepEqual(c.args, []string{"add"}) || c.stdin != "Linwifi2_1k\ns3cret pass\"x\n" {
		t.Fatalf("add call %+v", c)
	}
	if strings.Contains(w.Body.String(), "s3cret") {
		t.Fatalf("reply carries the password: %s", w.Body)
	}
}

func TestForget(t *testing.T) {
	calls := fake(t, "ok: forgot it; it counts from the robot's next boot\n", "", 0)
	if w := do(http.MethodPost, `{"action":"forget","hex":"4c696e77696669325f316b"}`); w.Code != http.StatusOK {
		t.Fatalf("forget: %d %s", w.Code, w.Body)
	}
	if want := []string{"forget", "4c696e77696669325f316b"}; !reflect.DeepEqual((*calls)[0].args, want) {
		t.Fatalf("forget args %v", (*calls)[0].args)
	}
}

func TestExitCodes(t *testing.T) {
	for _, tc := range []struct {
		code   int
		errOut string
		status int
	}{
		{ExitConnected, "wifi: the robot is on this network now; it is left as is\n", http.StatusConflict},
		{1, "wifi: SSH to 192.168.1.76 failed: timeout\n", http.StatusBadGateway},
		{-1, "", http.StatusBadGateway},
	} {
		fake(t, "", tc.errOut, tc.code)
		w := do(http.MethodPost, `{"action":"forget","hex":"41"}`)
		if w.Code != tc.status || (tc.errOut != "" && !strings.Contains(w.Body.String(), strings.TrimSpace(tc.errOut))) {
			t.Errorf("exit %d: %d %q", tc.code, w.Code, w.Body)
		}
	}
}

func TestRejectsBadInputWithoutRunning(t *testing.T) {
	calls := fake(t, "", "", 0)
	long := strings.Repeat("x", 33)
	for _, body := range []string{
		`{"action":"add","ssid":"","password":"12345678"}`,
		`{"action":"add","ssid":"` + long + `","password":"12345678"}`,
		`{"action":"add","ssid":"a\nb","password":"12345678"}`,
		`{"action":"add","ssid":"home","password":"short"}`,
		`{"action":"add","ssid":"home","password":"pässword1"}`,
		`{"action":"add","ssid":"home","password":"line\nbreak1"}`,
		`{"action":"add","ssid":"home","password":"` + strings.Repeat("a", 64) + `x"}`,
		`{"action":"forget","hex":"4C"}`,
		`{"action":"forget","hex":"4; reboot"}`,
		`{"action":"reboot"}`,
		`{"action":"add","ssid":"home","password":"12345678","extra":1}`,
	} {
		if w := do(http.MethodPost, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", body, w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodPost, API, strings.NewReader(`{"action":"forget","hex":"41"}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	Handler(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d", w.Code)
	}
	if len(*calls) != 0 {
		t.Fatalf("ran wifi.sh on bad input: %+v", *calls)
	}
}

func TestAccepts64HexKey(t *testing.T) {
	fake(t, "ok\n", "", 0)
	if w := do(http.MethodPost, `{"action":"add","ssid":"home","password":"`+strings.Repeat("aB", 32)+`"}`); w.Code != http.StatusOK {
		t.Fatalf("64-hex key: %d %s", w.Code, w.Body)
	}
}
