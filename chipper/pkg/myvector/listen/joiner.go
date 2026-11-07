package listen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// C2 capture: listen.sh POSTs each clip here; MaxClips closes an utterance nobody pauses in.
const (
	ClipAPI    = "/myvector/listen/clip"
	ClipMS     = 10000 // listen.sh CLIP_MS default
	MaxClips   = 3
	tailBytes  = 2 * 32000 // 2 s of 16 kHz s16le mono
	maxOverlap = 8         // words the 2 s tail can repeat
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

// Joiner turns clips into utterances; Heard runs under its lock, so it must not call Clip.
type Joiner struct {
	STT   func(pcm []byte) (string, error)
	Heard func(Utterance)
	mu    sync.Mutex
	bots  map[string]*joined
}

type joined struct {
	tail       []byte   // the previous clip's last 2 s
	prev       []string // words of the previous tail+clip transcript
	held       []string // the open utterance
	start, end time.Time
	clips      int
}

// Clip adds one clip that started at start; a closed utterance goes to Heard.
func (j *Joiner) Clip(esn string, start time.Time, pcm []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.bots == nil {
		j.bots = map[string]*joined{}
	}
	b := j.bots[esn]
	if b == nil {
		b = &joined{}
		j.bots[esn] = b
	}
	text, err := j.STT(append(append([]byte{}, b.tail...), pcm...))
	if err != nil {
		return err
	}
	last := ""
	tail := pcm
	if len(pcm) > tailBytes {
		tail = pcm[len(pcm)-tailBytes:]
	}
	if text != "" {
		if last, err = j.STT(tail); err != nil {
			return err
		}
	}
	words := strings.Fields(text)
	if fresh := words[overlap(b.prev, words):]; len(fresh) > 0 {
		if len(b.held) == 0 {
			b.start = start
		}
		b.held = append(b.held, fresh...)
		b.clips++
	}
	b.end = start.Add(time.Duration(len(pcm)) * time.Second / 32000)
	b.tail, b.prev = append([]byte{}, tail...), words
	if len(b.held) > 0 && (strings.TrimSpace(last) == "" || b.clips >= MaxClips) {
		u := Utterance{ESN: esn, Text: strings.Join(b.held, " "), Start: b.start, End: b.end}
		b.held, b.clips = nil, 0
		j.Heard(u)
	}
	return nil
}

// Drop forgets esn's open utterance and tail.
func (j *Joiner) Drop(esn string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.bots, esn)
}

// overlap is how many leading words of cur repeat prev's trailing words.
func overlap(prev, cur []string) int {
	k := maxOverlap
	if len(prev) < k {
		k = len(prev)
	}
	if len(cur) < k {
		k = len(cur)
	}
	for ; k > 0; k-- {
		same := true
		for i := 0; i < k && same; i++ {
			same = norm(prev[len(prev)-k+i]) == norm(cur[i])
		}
		if same {
			return k
		}
	}
	return 0
}

func norm(w string) string { return strings.ToLower(strings.TrimFunc(w, unicode.IsPunct)) }

// ClipHandler takes loopback POSTs ?esn=&start=<unix ms> with a pcm body: 204, or 410 once esn stops listening.
func ClipHandler(s *State, j *Joiner, logf func(string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		esn := r.URL.Query().Get("esn")
		ms, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		if esn == "" || err != nil {
			http.Error(w, "want ?esn=&start=<unix ms>", http.StatusBadRequest)
			return
		}
		if !s.Active(esn, time.Now()) {
			j.Drop(esn)
			w.WriteHeader(http.StatusGone)
			return
		}
		pcm, err := io.ReadAll(io.LimitReader(r.Body, 1<<21))
		if err == nil {
			err = j.Clip(esn, time.UnixMilli(ms), pcm)
		}
		if err != nil {
			logf("myvector listen: clip: " + err.Error())
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
