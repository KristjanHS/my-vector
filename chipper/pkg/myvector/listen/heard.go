package listen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	openai "github.com/sashabaranov/go-openai"
)

const (
	BackoffReset = 5 * time.Minute  // A9: this long without a listen reply restarts Backoff
	TurnTail     = 10 * time.Second // A6: a wake-word turn's echo window after the ask
	EchoPad      = 1 * time.Second
	Window       = 2 * time.Minute // A5: rolling transcript
	askWait      = 60 * time.Second
)

// Backoff is the A9 wait after the 1st, 2nd, 3rd, 4th+ listen reply in a row.
var Backoff = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}

// named spots Vector's name in overheard text (et case endings included); it skips the Backoff wait.
var named = regexp.MustCompile(`(?i)\b(?:vector|victor|vektor)\w*`)

// Persona and SpeakRules also build listenprobe's prompt 1 arm.
const (
	SpeakRules = "Speak only when: someone asks a question nobody has answered, someone states a clear factual mistake, someone mentions you (Vector) by name, or there is a clear opening for a short fun remark."
	QuietRules = "Stay quiet for small talk, one side of a phone call, TV or radio, private or emotional talk, unfinished sentences, and questions someone already answered. When unsure, stay quiet."
	Persona    = "You are Vector, a small home robot. You overhear people talking nearby as speech-to-text lines, with no speaker names. "
)

// Prompt is listenprobe "prompt 2" (P1).
const Prompt = Persona + SpeakRules + " " + QuietRules +
	" To stay quiet, reply with the single word SKIP. To speak, reply with one or two short, friendly sentences in the same language as the conversation. Never explain your choice."

// User is the user message: earlier lines, then the newest one.
func User(before []string, now string) string {
	var b strings.Builder
	b.WriteString("Earlier:\n")
	if len(before) == 0 {
		b.WriteString("(nothing)\n")
	}
	for _, l := range before {
		b.WriteString(l + "\n")
	}
	b.WriteString("Newest line:\n" + now)
	return b.String()
}

// Parse reports whether reply speaks: empty or a SKIP prefix (any case) stays quiet.
func Parse(reply string) bool {
	s := strings.ToUpper(strings.TrimSpace(reply))
	return s != "" && !strings.HasPrefix(s, "SKIP")
}

// ReplyCap is the listen reply's max_tokens before any thinking budget.
const ReplyCap = 80

// Ask is one Prompt call, thinking per llm.Config.Thinking for PathListen.
func Ask(ctx context.Context, c *openai.Client, model, user string) (string, error) {
	r := openai.ChatCompletionRequest{Model: model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: Prompt}, {Role: openai.ChatMessageRoleUser, Content: user}}}
	llm.Load().Thinking(llm.Ollama, model, llm.PathListen).Apply(&r, ReplyCap)
	res, err := c.CreateChatCompletion(ctx, r)
	if err != nil || len(res.Choices) == 0 {
		return "", fmt.Errorf("no answer: %v", err)
	}
	return strings.TrimSpace(res.Choices[0].Message.Content), nil
}

// Confirm is the spoken on/off confirmation.
func Confirm(on, estonian bool) string {
	return map[bool]map[bool]string{
		false: {true: "Active listening on.", false: "Active listening off."},
		true:  {true: "Aktiivne kuulamine sees.", false: "Aktiivne kuulamine väljas."},
	}[estonian][on]
}

// Utterance is one overheard sentence; Start/End bound its clips on the server clock.
type Utterance struct {
	ESN, Text  string
	Start, End time.Time
}

// Hearer decides each overheard utterance; nil Locked/Quiet/Now/Estonian mean unlocked, no-op, time.Now, English.
type Hearer struct {
	State    *State
	Client   func() (*openai.Client, string, bool)
	Say      func(esn, text string) error
	Locked   func(now time.Time) bool
	Quiet    func(esn, phrase string)
	Estonian func() bool
	Now      func() time.Time
	LogDir   string // A8 MYVECTOR_LISTEN_LOG
}

func (h *Hearer) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Heard runs A2/A3/A5/A6/A9 on u: phrases, echo gate, cooldown, one LLM call, then maybe one reply.
func (h *Hearer) Heard(u Utterance) {
	s, now := h.State, h.now()
	if !s.Active(u.ESN, now) {
		return
	}
	if p, ok := quiet.Match(u.Text); ok {
		s.Deactivate(u.ESN, "quiet")
		if h.Quiet != nil {
			h.Quiet(u.ESN, p)
		}
		return
	}
	if on, ok := Match(u.Text); ok {
		if !on {
			s.Deactivate(u.ESN, "phrase")
			h.speak(u.ESN, Confirm(false, h.Estonian != nil && h.Estonian()))
		}
		return
	}
	if s.Echo(u.ESN, u.Start, u.End) {
		s.Log("myvector listen: echo dropped")
		return
	}
	s.Heard(u.ESN, now)
	before := s.hear(u.ESN, now, u.Text)
	words := len(strings.Fields(u.Text))
	if why := s.claim(u.ESN, now, named.MatchString(u.Text)); why != "" {
		s.Log(fmt.Sprintf("myvector listen: %d words → %s", words, why))
		return
	}
	defer s.release(u.ESN)
	c, model, ok := h.Client()
	if !ok {
		s.Log("myvector listen: no Ollama target")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), askWait)
	defer cancel()
	reply, err := Ask(ctx, c, model, User(before, u.Text))
	if err != nil {
		s.Log("myvector listen: " + err.Error())
		return
	}
	speak := Parse(reply)
	verdict := map[bool]string{false: "SKIP", true: "SPEAK"}[speak]
	s.Log(fmt.Sprintf("myvector listen: %d words → %s %dms", words, verdict, h.now().Sub(now).Milliseconds()))
	h.logText(now, verdict, u.Text)
	if speak && s.Active(u.ESN, h.now()) {
		if h.speak(u.ESN, reply) {
			s.Spoke(u.ESN, h.now())
		}
	}
}

// speak says text unless the quiet lock holds, and stamps its echo window.
func (h *Hearer) speak(esn, text string) bool {
	from := h.now()
	if h.Locked != nil && h.Locked(from) {
		h.State.Log("myvector listen: reply dropped (quiet lock)")
		return false
	}
	err := h.Say(esn, text)
	h.State.Spoken(esn, from, h.now())
	if err != nil {
		h.State.Log("myvector listen: say: " + err.Error())
	}
	return err == nil
}

func (h *Hearer) logText(now time.Time, verdict, text string) {
	if h.LogDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(h.LogDir, now.Format("2006-01-02")+".txt"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		h.State.Log("myvector listen: log: " + err.Error())
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", verdict, text)
}
