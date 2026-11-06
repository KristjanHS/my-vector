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
		if done, _ := req.DetectEndOfSpeech(); done {
			return req.DecodedMicData, nil
		}
	}
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
	logger.Println("Bot " + req.Device + " Transcribed text (" + l + "): " + text)
	return text, nil
}
