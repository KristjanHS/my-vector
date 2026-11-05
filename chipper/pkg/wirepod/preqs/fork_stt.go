package processreqs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
)

// forkSTTURL is the myvector-stt service (et-stt/server.py): 16 kHz s16le mono PCM in, {"text"} out.
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

// forkSTTRouter keeps h for English; lang et-EE goes to myvector-stt, and its failure is an error, never h (plan E20).
func forkSTTRouter(h func(sr.SpeechRequest) (string, error)) func(sr.SpeechRequest) (string, error) {
	if h == nil {
		return nil
	}
	return func(req sr.SpeechRequest) (string, error) {
		if lang.Current() != lang.Estonian {
			return h(req)
		}
		return forkEstonianSTT(req)
	}
}

func forkEstonianSTT(req sr.SpeechRequest) (string, error) {
	pcm, err := forkSTTCollect(req)
	if err != nil {
		return "", err
	}
	resp, err := forkSTTClient.Post(forkSTTURL, "application/octet-stream", bytes.NewReader(pcm))
	if err != nil {
		return "", fmt.Errorf("et STT unavailable: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", fmt.Errorf("et STT read: %w", err)
	}
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	jerr := json.Unmarshal(body, &out)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("et STT %s: %s", resp.Status, out.Error)
	}
	if jerr != nil {
		return "", fmt.Errorf("et STT reply: %w", jerr)
	}
	text := strings.ToLower(strings.TrimSpace(out.Text))
	logger.Println("Bot " + req.Device + " Transcribed text (et-EE): " + text)
	return text, nil
}
