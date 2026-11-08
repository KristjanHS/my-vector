package processreqs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
)

// forkSTTURL is the myvector-stt service (et-stt/server.py): 16 kHz s16le mono PCM in, {"text"} out; ?lang= picks the model.
var forkSTTURL = "http://127.0.0.1:8092/stt"

// 60 s covers a request that waits for the model load (plan E21).
var forkSTTClient = &http.Client{Timeout: 60 * time.Second}

// forkSTTCollect reads the request's decoded PCM up to end of speech, as stt/whisper does.
var forkSTTCollect = func(req sr.SpeechRequest) ([]byte, error) {
	for {
		if _, err := req.GetNextStreamChunk(); err != nil {
			return nil, err
		}
		if forkEndOfSpeech(&req) {
			return req.DecodedMicData, nil
		}
	}
}

// forkEndSilence ends a "Hey Vector" request after this much VAD silence (user ruling 2026-11-08: 700 ms, the
// active-listening pause cut P1). Upstream DetectEndOfSpeech stops at 230 ms and cut sentences at a comma.
const forkEndSilence = 700 * time.Millisecond

// forkMinSpeechFrames is upstream's ActiveFrames > 18: no end before 190 ms of speech.
const forkMinSpeechFrames = 19

// forkVADActive is one 10 ms frame's VAD verdict (a seam for tests).
var forkVADActive = func(req *sr.SpeechRequest, frame []byte) (bool, error) { return req.VADInst.Process(16000, frame) }

// forkEndOfSpeech is sr.DetectEndOfSpeech with a forkEndSilence window, over req.LastAudioChunk's 10 ms frames.
func forkEndOfSpeech(req *sr.SpeechRequest) bool {
	need := int(forkEndSilence / (10 * time.Millisecond))
	for _, frame := range sr.SplitVAD(req.LastAudioChunk) {
		active, err := forkVADActive(req, frame)
		if err != nil {
			logger.Println("VAD err: " + err.Error())
			return true
		}
		if active {
			req.ActiveFrames++
			req.InactiveFrames = 0
		} else {
			req.InactiveFrames++
		}
		if req.InactiveFrames >= need && req.ActiveFrames >= forkMinSpeechFrames {
			logger.Println("(Bot " + req.Device + ") End of speech detected.")
			return true
		}
	}
	return false
}

// forkSTTRouter sends every lang to myvector-stt (?lang=); its failure is an error, never h (Vosk), as plan E20 ruled for
// et-EE. h == nil (no STT configured upstream) still returns nil.
func forkSTTRouter(h func(sr.SpeechRequest) (string, error)) func(sr.SpeechRequest) (string, error) {
	if h == nil {
		return nil
	}
	return func(req sr.SpeechRequest) (string, error) { return forkServiceSTT(req, lang.Current()) }
}

func forkServiceSTT(req sr.SpeechRequest, l string) (string, error) {
	pcm, err := forkSTTCollect(req)
	if err != nil {
		return "", err
	}
	resp, err := forkSTTClient.Post(forkSTTURL+"?lang="+url.QueryEscape(l), "application/octet-stream", bytes.NewReader(pcm))
	if err != nil {
		return "", fmt.Errorf("%s STT unavailable: %w", l, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("%s STT read: %w", l, err)
	}
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	jerr := json.Unmarshal(body, &out)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s STT %s: %s", l, resp.Status, out.Error)
	}
	if jerr != nil {
		return "", fmt.Errorf("%s STT reply: %w", l, jerr)
	}
	text := strings.ToLower(strings.TrimSpace(out.Text))
	if l == lang.English {
		text = forkVoskShape(text)
	}
	logger.Println("Bot " + req.Device + " Transcribed text (" + l + "): " + text)
	return text, nil
}

// forkVoskShape turns Whisper's English into Vosk's shape, which upstream's timer/weather parsers expect: letters, digits,
// apostrophes and single spaces only ("10-minute timer, please!" -> "10 minute timer please"); a '.' between digits stays.
func forkVoskShape(s string) string {
	r := []rune(s)
	for i, c := range r {
		if c == '’' { // Whisper's typographic apostrophe
			r[i], c = '\'', '\''
		}
		keep := unicode.IsLetter(c) || unicode.IsDigit(c) || c == '\'' || c == ' '
		if c == '.' && i > 0 && i+1 < len(r) && unicode.IsDigit(r[i-1]) && unicode.IsDigit(r[i+1]) {
			keep = true
		}
		if !keep {
			r[i] = ' '
		}
	}
	return strings.Join(strings.Fields(string(r)), " ")
}
