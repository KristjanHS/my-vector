package wirepod_ttr

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/robotconn"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
	"golang.org/x/text/unicode/norm"
)

// forkVisionSay speaks forkVisionDown's and forkPhotoShot's lines; a var so tests fake it.
var forkVisionSay = forkSayText

// forkVisionDown is DoGetImage's stream-create error hook (plan vision-setting V8): one native line, never a retry.
func forkVisionDown(robot *vector.Vector) {
	line := "I can't see right now."
	if lang.Current() == lang.Estonian {
		line = "Ma ei näe praegu."
	}
	forkVisionSay(line, robot)
}

// forkPhotoActs drops a photo answer's getImage actions (V15): one photo per ask.
func forkPhotoActs(acts []RobotAction) []RobotAction {
	out := acts[:0:0]
	for _, a := range acts {
		if a.Action != ActionGetImage {
			out = append(out, a)
		}
	}
	return out
}

// forkPhotoPhrases per language (V16, V16a): anywhere matches inside the utterance, start only at its start.
var forkPhotoPhrases = map[string]struct{ anywhere, start []string }{
	lang.Estonian: {[]string{"mida sa näed", "mis sa näed", "tee pilt", "tee foto"}, []string{"vaata"}},
	lang.English:  {[]string{"what do you see", "what can you see", "take a picture", "take a photo"}, []string{"look at"}},
}

// forkPhotoNames are the robot's name as STT writes it, stripped (after an optional hey / hei) before a start phrase.
var forkPhotoNames = map[string]bool{"vector": true, "vektor": true}

// forkPhotoAsk: text holds one of l's photo phrases, NFC-composed, case-folded and punctuation-stripped, whole words
// (V16, V16a); a combining mark stays inside its word.
func forkPhotoAsk(l, text string) bool {
	w := strings.FieldsFunc(strings.ToLower(norm.NFC.String(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r)
	})
	p := forkPhotoPhrases[l]
	for _, ph := range p.anywhere {
		if strings.Contains(" "+strings.Join(w, " ")+" ", " "+ph+" ") {
			return true
		}
	}
	if len(w) > 1 && (w[0] == "hey" || w[0] == "hei") && forkPhotoNames[w[1]] {
		w = w[2:]
	} else if len(w) > 0 && forkPhotoNames[w[0]] {
		w = w[1:]
	}
	for _, ph := range p.start {
		if strings.HasPrefix(strings.Join(w, " ")+" ", ph+" ") {
			return true
		}
	}
	return false
}

// forkPhotoRobot is esn's robot as StreamingKGSim dials and probes it; a var so tests fake it.
var forkPhotoRobot = func(esn string) (*vector.Vector, error) {
	for _, bot := range vars.BotInfo.Robots {
		if bot.Esn != esn {
			continue
		}
		robot, err := robotconn.Get(esn, bot.GUID, bot.IPAddress+":443")
		if err != nil {
			return nil, err
		}
		if _, err := robot.Conn.BatteryState(context.Background(), &vectorpb.BatteryStateRequest{}); err != nil {
			return nil, err
		}
		return robot, nil
	}
	return nil, errors.New("robot " + esn + " unknown")
}

// forkPhotoIntent answers a non-KG turn's intent request as StreamingKGSim's first sentence does; a var so tests fake it.
var forkPhotoIntent = IntentPass

// forkPhotoTurn is StreamingKGSim's phrase trigger (V16): a matched request runs forkPhotoShot under StreamingKGSim's
// control, interrupter and speak-loop end, with no brain hop; true = handled. A dial error falls through. A non-KG
// request (intent_graph's unmatched utterance) first gets StreamingKGSim's intent_greeting_hello answer, then control.
func forkPhotoTurn(req interface{}, esn string, isKG bool, text string) bool {
	l := lang.Current()
	if !forkPhotoAsk(l, text) {
		return false
	}
	robot, err := forkPhotoRobot(esn)
	if err != nil {
		logger.Println("myvector vision: phrase ask " + l + ": " + err.Error())
		return false
	}
	logger.Println("myvector vision: phrase ask " + l)
	msgs := CreateAIReq(text, esn, false, isKG).Messages // LLM-marks the ask before IntentPass captures it (M4)
	start, stop, stopStop := make(chan bool), make(chan bool), make(chan bool, 1)
	if !isKG {
		forkPhotoIntent(req, "intent_greeting_hello", text, map[string]string{}, false)
		time.Sleep(time.Millisecond * 200)
	}
	defer forkBControl(robot, context.Background(), start, stop, esn)()
	var interrupted atomic.Bool
	ibot, istop, endInterrupt := forkInterruptBot(robot, stop)
	defer close(stopStop)
	defer endInterrupt()
	shot := forkPhotoStop(forkSpeakCtx(robot))
	defer close(shot.quit)
	go func() { interrupted.Store(InterruptKGSimWhenTouchedOrWaked(ibot, istop, stopStop)) }()
	for range start {
		forkPhotoShot(robot, l, msgs, shot.stop)
		if !interrupted.Load() {
			stopStop <- true
			stop <- true
		}
	}
	return true
}

// forkPhotoShotStop is DoGetImage's stop for a phrase turn: the interrupter's stop never sends on stopStop, so the
// interrupt's speech cut sends here instead; closing quit (the turn ended) closes stop.
type forkPhotoShotStop struct {
	stop chan bool
	quit chan struct{}
}

func forkPhotoStop(cut context.Context) forkPhotoShotStop {
	s := forkPhotoShotStop{make(chan bool, 1), make(chan struct{})}
	go func() {
		select {
		case <-cut.Done():
			s.stop <- true
			<-s.quit
		case <-s.quit:
		}
		close(s.stop)
	}()
	return s
}

// forkPhotoShot speaks the V18 line, then DoGetImage on the request's messages; DoGetImage remembers the turn.
func forkPhotoShot(robot *vector.Vector, l string, msgs []openai.ChatCompletionMessage, stop chan bool) {
	line := "Let me look."
	if l == lang.Estonian {
		line = "Vaatan."
	}
	forkVisionSay(line, robot)
	DoGetImage(msgs, "front", robot, stop)
}

// forkPhotoData is DoGetImage's capture result: a failed capture speaks the V8 line and sets *stop, so DoGetImage
// returns before the vision call instead of panicking on a nil response.
func forkPhotoData(resp *vectorpb.CaptureSingleImageResponse, robot *vector.Vector, stop *bool) []byte {
	if resp == nil || len(resp.Data) == 0 {
		logger.Println("myvector vision: capture failed")
		*stop = true
		forkVisionDown(robot)
		return nil
	}
	return resp.Data
}

// forkPhotoWake is DoGetImage's stream-EOF wake, as rep.wake is StreamingKGSim's: the speak loop can check isDone,
// miss the last sentence's non-blocking send, then wait on speakReady forever.
func forkPhotoWake(speakReady chan string) { new(forkRepeats).wake(speakReady) }

// forkPhotoFailed is DoGetImage's non-EOF stream-error hook: the stream is done and the speak loop woken — upstream
// returned without either, and the loop waited forever holding control and the answer slot.
func forkPhotoFailed(isDone *bool, speakReady chan string) {
	*isDone = true
	forkPhotoWake(speakReady)
}

// forkPhotoEmpty is DoGetImage's stream-EOF guard before it joins fullRespSlice (upstream indexed [0]): an empty reply
// wakes the speak loop and is not remembered; the loop's end speaks the V8 line (forkPhotoUnsaid).
func forkPhotoEmpty(chunks []string, speakReady chan string) bool {
	if len(chunks) > 0 {
		return false
	}
	logger.Println("myvector vision: empty reply")
	forkPhotoWake(speakReady)
	return true
}

// forkPhotoUnsaid is DoGetImage's speak-loop end: a reply that spoke no sentence (empty, or a stream error before its
// first) gets the V8 line, unless an interrupt ended the loop.
func forkPhotoUnsaid(spoken int, stopped <-chan struct{}, robot *vector.Vector) {
	select {
	case <-stopped:
		return
	default:
	}
	if spoken == 0 {
		forkVisionDown(robot)
	}
}

// forkPhotoRemember is DoGetImage's stream-EOF Remember: a camera turn is remembered as (the user's text ask, the
// answer), with or without SaveChat as StreamingKGSim + forkAfterReply remember a turn (F3) — never the image
// message, never the V18 line. msgs end in the image; a brain-decided getImage has the brain's reply before it, and
// the answer extends the (ask, reply) pair the brain's turn remembered instead of adding a second ask. An interrupted
// turn (stopped closed) is not remembered: its answer was cut off. Always true: upstream's image-message Remember
// never runs.
func forkPhotoRemember(msgs []openai.ChatCompletionMessage, answer, esn string, stopped <-chan struct{}) bool {
	select {
	case <-stopped:
		logger.Println("myvector vision: interrupted, turn not remembered")
		return true
	default:
	}
	n := len(msgs) - 1 // the image
	brain := n > 0 && msgs[n-1].Role == openai.ChatMessageRoleAssistant
	if brain {
		n--
	}
	if n < 1 || msgs[n-1].Role != openai.ChatMessageRoleUser || msgs[n-1].Content == "" {
		logger.Println("myvector vision: no text ask before the image, turn not remembered")
		return true
	}
	ask := msgs[n-1].Content
	if c := GetChat(esn); brain && len(c.Chats) >= 2 {
		u, a := &c.Chats[len(c.Chats)-2], &c.Chats[len(c.Chats)-1]
		if u.Role == openai.ChatMessageRoleUser && u.Content == ask && a.Role == openai.ChatMessageRoleAssistant {
			forkCaptureChat(ask, answer, esn)
			a.Content = strings.TrimSpace(a.Content + " " + answer)
			PlaceChat(c)
			return true
		}
	}
	Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: ask},
		openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: answer}, esn)
	return true
}
