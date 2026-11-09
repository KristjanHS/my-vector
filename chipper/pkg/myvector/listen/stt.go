package listen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// STTURL is myvector-stt, as processreqs forkSTTURL.
var STTURL = "http://127.0.0.1:8092/stt"

var sttClient = &http.Client{Timeout: 60 * time.Second}

// STT transcribes 16 kHz s16le mono pcm in lang (en-US, et-EE).
func STT(lang string, pcm []byte) (string, error) {
	resp, err := sttClient.Post(STTURL+"?lang="+url.QueryEscape(lang), "application/octet-stream", bytes.NewReader(pcm))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt %s", resp.Status)
	}
	var out struct{ Text string }
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&out)
	return strings.TrimSpace(out.Text), err
}
