package processreqs

import (
	"bytes"
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

func TestForkSTTRouterEnglishUsesWrapped(t *testing.T) {
	lang.UseForTest(t, lang.English)
	vosk, calls := forkSTTStub(t, "http://127.0.0.1:1/stt")
	forkSTTCollect = func(sr.SpeechRequest) ([]byte, error) { t.Fatal("English read audio for et STT"); return nil, nil }
	text, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
	if err != nil || text != "what time is it" || *calls != 1 {
		t.Fatalf("got %q, %v, vosk calls %d", text, err, *calls)
	}
}

func TestForkSTTRouterEstonianPosts(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/stt" || !bytes.Equal(body, forkTestPCM) {
			t.Errorf("got %s %s body %v", r.Method, r.URL.Path, body)
		}
		io.WriteString(w, `{"text": " Mis kell ÕHTUL on? "}`)
	}))
	defer srv.Close()
	vosk, calls := forkSTTStub(t, srv.URL+"/stt")
	text, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
	if err != nil || text != "mis kell õhtul on?" || *calls != 0 {
		t.Fatalf("got %q, %v, vosk calls %d", text, err, *calls)
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

func TestForkSTTRouterEstonianUnreachable(t *testing.T) {
	lang.UseForTest(t, lang.Estonian)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/stt"
	srv.Close()
	vosk, calls := forkSTTStub(t, url)
	_, err := forkSTTRouter(vosk)(sr.SpeechRequest{})
	if err == nil || !strings.Contains(err.Error(), "et STT unavailable") || *calls != 0 {
		t.Fatalf("got %v, vosk calls %d", err, *calls)
	}
}
