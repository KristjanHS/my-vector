package processreqs

import (
	"bytes"
	"encoding/json"
	"errors"
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

// forkEndAt feeds frames (true = speech) one 10 ms frame per chunk through forkEndOfSpeech and returns the index of
// the frame that ended the request, or -1.
func forkEndAt(t *testing.T, frames []bool) int {
	t.Helper()
	old := forkVADActive
	forkVADActive = func(_ *sr.SpeechRequest, f []byte) (bool, error) { return f[0] == 1, nil }
	t.Cleanup(func() { forkVADActive = old })
	var req sr.SpeechRequest
	for i, speech := range frames {
		f := make([]byte, 320)
		if speech {
			f[0] = 1
		}
		req.LastAudioChunk = f
		if forkEndOfSpeech(&req) {
			return i
		}
	}
	return -1
}

func forkFrames(parts ...any) []bool { // pairs: bool speech, int ms
	var out []bool
	for i := 0; i < len(parts); i += 2 {
		for n := 0; n < parts[i+1].(int)/10; n++ {
			out = append(out, parts[i].(bool))
		}
	}
	return out
}

// A 400 ms comma pause no longer ends the request; 700 ms of trailing silence does, on its 70th frame.
func TestForkEndOfSpeechKeepsCommaPause(t *testing.T) {
	frames := forkFrames(true, 300, false, 400, true, 300, false, 1000)
	if got, want := forkEndAt(t, frames), 300/10+400/10+300/10+700/10-1; got != want {
		t.Fatalf("end at frame %d, want %d (700 ms after the last speech)", got, want)
	}
}

// Under 190 ms of speech never ends, however long the silence (upstream's ActiveFrames > 18).
func TestForkEndOfSpeechNeedsSpeech(t *testing.T) {
	if got := forkEndAt(t, forkFrames(false, 500, true, 180, false, 2000)); got != -1 {
		t.Fatalf("ended at frame %d after 180 ms of speech, want no end", got)
	}
	if got := forkEndAt(t, forkFrames(true, 190, false, 700)); got != 19+70-1 {
		t.Fatalf("190 ms of speech: end at frame %d, want %d", got, 19+70-1)
	}
}

// forkCollectFrames runs forkSTTCollect over a fake stream: one 10 ms frame per chunk (true = speech; its PCM byte
// is the frame number), then io.EOF. It returns the PCM (one byte per frame read) and the error.
func forkCollectFrames(t *testing.T, frames []bool) ([]byte, error) {
	t.Helper()
	oldNext, oldVAD := forkNextChunk, forkVADActive
	forkVADActive = func(_ *sr.SpeechRequest, f []byte) (bool, error) { return f[0] == 1, nil }
	i := 0
	forkNextChunk = func(req *sr.SpeechRequest) ([]byte, error) {
		if i == len(frames) {
			return nil, io.EOF
		}
		f := make([]byte, 320)
		if frames[i] {
			f[0] = 1
		}
		req.LastAudioChunk = f
		req.DecodedMicData = append(req.DecodedMicData, byte(i))
		i++
		return f, nil
	}
	t.Cleanup(func() { forkNextChunk, forkVADActive = oldNext, oldVAD })
	return forkSTTCollect(sr.SpeechRequest{})
}

// A sentence still running at the robot's 6 s cap (EOF, no 700 ms pause yet) goes to STT whole; a near-empty one
// that hits EOF stays an error; a pause ends the read before EOF.
func TestForkSTTCollectEOF(t *testing.T) {
	pcm, err := forkCollectFrames(t, forkFrames(true, 5600, false, 400))
	if err != nil || len(pcm) != 600 {
		t.Fatalf("speech to the cap: got %d frames, err %v; want 600, nil", len(pcm), err)
	}
	if _, err := forkCollectFrames(t, forkFrames(true, 100, false, 300)); !errors.Is(err, io.EOF) {
		t.Fatalf("100 ms of speech then EOF: err %v, want io.EOF", err)
	}
	pcm, err = forkCollectFrames(t, forkFrames(true, 1000, false, 2000))
	if err != nil || len(pcm) != 170 {
		t.Fatalf("1 s speech + pause: got %d frames, err %v; want 170 (ends 700 ms in), nil", len(pcm), err)
	}
}
