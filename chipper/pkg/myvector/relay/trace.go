package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TraceDir is where each camera relay turn saves its steps: $MYVECTOR_VISION_TRACE_DIR, else robot/work/vision-trace
// (repo root or chipper/ cwd); "" (tracing off) while the dir does not exist.
func TraceDir() string {
	if d := os.Getenv("MYVECTOR_VISION_TRACE_DIR"); d != "" {
		if isDir(d) {
			return d
		}
		return ""
	}
	for _, d := range []string{"robot/work/vision-trace", "../robot/work/vision-trace"} {
		if isDir(d) {
			return d
		}
	}
	return ""
}

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

// Trace is one relay turn's folder: trace.md (every hop's exact request and reply, in order) and photo.jpg (the
// image as the vision model got it). A nil *Trace is off; write errors are dropped, a trace never fails the turn.
type Trace struct {
	Dir   string
	photo int
}

// NewTrace opens <TraceDir>/<stamp>/, nil when tracing is off.
func NewTrace(now time.Time) *Trace {
	root := TraceDir()
	if root == "" {
		return nil
	}
	d := filepath.Join(root, now.Format(traceStamp))
	if os.MkdirAll(d, 0o755) != nil {
		return nil
	}
	prune(root)
	return &Trace{Dir: d}
}

const traceStamp = "20060102-150405.000"

// TraceKeep is how many turn folders TraceDir keeps; NewTrace deletes the oldest beyond it.
var TraceKeep = 50

func prune(root string) {
	es, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var turns []string
	for _, e := range es {
		if _, err := time.Parse(traceStamp, e.Name()); err == nil && e.IsDir() {
			turns = append(turns, e.Name()) // ReadDir sorts by name = by time
		}
	}
	for len(turns) > TraceKeep {
		os.RemoveAll(filepath.Join(root, turns[0]))
		turns = turns[1:]
	}
}

func (t *Trace) add(s string) {
	if t == nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(t.Dir, "trace.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(s)
}

// Note appends a heading and text.
func (t *Trace) Note(head, text string) { t.add("## " + head + "\n\n" + fence(text) + "\n") }

// Request appends step's request body (any JSON-marshalable chat request): its parameters, then each message in
// full. An image part is saved as photo[-N].jpg and shown by name.
func (t *Trace) Request(step string, body any) {
	if t == nil {
		return
	}
	var m map[string]any
	if b, err := json.Marshal(body); err != nil || json.Unmarshal(b, &m) != nil {
		t.Note(step+" request", fmt.Sprint(body))
		return
	}
	msgs, _ := m["messages"].([]any)
	delete(m, "messages")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var s strings.Builder
	s.WriteString("## " + step + " request\n\n")
	for _, k := range keys {
		v, _ := json.Marshal(m[k])
		s.WriteString("- " + k + ": `" + string(v) + "`\n")
	}
	for i, raw := range msgs {
		msg, _ := raw.(map[string]any)
		role, _ := msg["role"].(string)
		fmt.Fprintf(&s, "\n### [%d] %s\n\n", i, role)
		switch c := msg["content"].(type) {
		case string:
			s.WriteString(fence(c))
		case []any:
			for _, p := range c {
				s.WriteString(t.part(p))
			}
		}
	}
	t.add(s.String() + "\n")
}

func (t *Trace) part(p any) string {
	pm, _ := p.(map[string]any)
	switch pm["type"] {
	case "text":
		txt, _ := pm["text"].(string)
		return "text part:\n" + fence(txt)
	case "image_url":
		iu, _ := pm["image_url"].(map[string]any)
		url, _ := iu["url"].(string)
		detail, _ := iu["detail"].(string)
		return fmt.Sprintf("image part: %s (detail %q)\n\n", t.savePhoto(url), detail)
	}
	b, _ := json.Marshal(p)
	return fence(string(b))
}

func (t *Trace) savePhoto(url string) string {
	_, b64, ok := strings.Cut(url, ";base64,")
	if !ok {
		return "non-data URL " + url
	}
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "undecodable image: " + err.Error()
	}
	name := "photo.jpg"
	if t.photo++; t.photo > 1 {
		name = fmt.Sprintf("photo-%d.jpg", t.photo)
	}
	if err := os.WriteFile(filepath.Join(t.Dir, name), b, 0o644); err != nil {
		return name + " (not saved: " + err.Error() + ")"
	}
	return fmt.Sprintf("[%s](%s), %d bytes", name, name, len(b)) + lastPhoto.seen(b, filepath.Base(t.Dir))
}

// lastPhoto is the previous traced photo: the same bytes again are a kept-photo follow-up
// (2026-11-10-photo-followup.md (docs/plans/) F5).
var lastPhoto photoSeen

type photoSeen struct {
	mu    sync.Mutex
	sum   [32]byte
	stamp string
}

// seen notes "reused photo from <stamp>" when b repeats the previous photo, else records b under stamp.
func (p *photoSeen) seen(b []byte, stamp string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	sum := sha256.Sum256(b)
	if p.stamp != "" && sum == p.sum {
		return ", reused photo from " + p.stamp
	}
	p.sum, p.stamp = sum, stamp
	return ""
}

// Reply appends step's reply: model, time, whether it hit its token cap, the text or the error.
func (t *Trace) Reply(step, model string, d time.Duration, reply string, cut bool, err error) {
	head := fmt.Sprintf("%s reply — %s, %d ms", step, model, d.Milliseconds())
	if cut {
		head += ", CUT at token cap"
	}
	if err != nil {
		t.Note(head, "ERROR: "+err.Error()+"\n"+reply)
		return
	}
	t.Note(head, reply)
}

func fence(s string) string { return "````\n" + s + "\n````\n" }
