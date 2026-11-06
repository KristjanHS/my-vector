package processreqs

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
)

var forkTestPCM = []byte{1, 0, 2, 0, 3, 0}

// forkSTTStub points the router at srv and stubs the audio read; it returns a "vosk" handler that counts its calls.
func forkSTTStub(t *testing.T, url string) (func(sr.SpeechRequest) (string, error), *int) {
	t.Helper()
	oldURL, oldCollect := forkSTTURL, forkSTTCollect
	forkSTTURL = url
	forkSTTCollect = func(sr.SpeechRequest) ([]byte, error) { return forkTestPCM, nil }
	t.Cleanup(func() { forkSTTURL, forkSTTCollect = oldURL, oldCollect })
	calls := 0
	return func(sr.SpeechRequest) (string, error) { calls++; return "what time is it", nil }, &calls
}

func TestForkSTTRouterPostsLang(t *testing.T) {
	for _, c := range []struct{ lang, reply, want string }{
		{lang.English, `{"text": " What time is it? "}`, "what time is it"},
		{lang.Estonian, `{"text": " Mis kell ÕHTUL on? "}`, "mis kell õhtul on?"},
	} {
		lang.UseForTest(t, c.lang)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || r.URL.Path != "/stt" || r.URL.Query().Get("lang") != c.lang || !bytes.Equal(body, forkTestPCM) {
				t.Errorf("%s: got %s %s body %v", c.lang, r.Method, r.URL, body)
			}
			io.WriteString(w, c.reply)
		}))
		vosk, calls := forkSTTStub(t, srv.URL+"/stt")
		text, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
		srv.Close()
		if err != nil || text != c.want || *calls != 0 {
			t.Fatalf("%s: got %q, %v, vosk calls %d", c.lang, text, err, *calls)
		}
	}
}

// English gets Vosk's shape for upstream's parsers (timer regex, weather location); Estonian text is only lowercased + trimmed.
func TestForkSTTVoskShape(t *testing.T) {
	for _, c := range []struct{ lang, text, want string }{
		{lang.English, "Set a 10-minute timer, please!", "set a 10 minute timer please"},
		{lang.English, "What's the weather in Paris, France?", "what's the weather in paris france"},
		{lang.English, "1.5 minutes. It’s 3.", "1.5 minutes it's 3"},
		{lang.English, " Hi...  there\t— you ", "hi there you"},
		{lang.Estonian, " Mis kell ÕHTUL on? ", "mis kell õhtul on?"},
	} {
		lang.UseForTest(t, c.lang)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]string{"text": c.text})
		}))
		vosk, _ := forkSTTStub(t, srv.URL+"/stt")
		text, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
		srv.Close()
		if err != nil || text != c.want {
			t.Errorf("%s %q: got %q, %v; want %q", c.lang, c.text, text, err, c.want)
		}
	}
}

func TestForkSTTRouterEstonianServerError(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error": "model load failed"}`)
	}))
	defer srv.Close()
	vosk, calls := forkSTTStub(t, srv.URL+"/stt")
	_, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
	if err == nil || !strings.Contains(err.Error(), "model load failed") || *calls != 0 {
		t.Fatalf("got %v, vosk calls %d", err, *calls)
	}
}

func TestForkSTTRouterUnreachableNeverFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/stt"
	srv.Close()
	for _, l := range []string{lang.English, lang.Estonian} {
		lang.UseForTest(t, l)
		vosk, calls := forkSTTStub(t, url)
		_, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
		if err == nil || !strings.Contains(err.Error(), l+" STT unavailable") || *calls != 0 {
			t.Fatalf("%s: got %v, vosk calls %d", l, err, *calls)
		}
	}
}
