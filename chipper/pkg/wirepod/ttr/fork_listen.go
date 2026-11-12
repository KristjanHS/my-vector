package wirepod_ttr

import (
	"context"
	"os"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
)

var forkListening = func() *listen.State {
	s := listen.NewState()
	s.Log = func(l string) { logger.Println(l) }
	return s
}()

func init() {
	forkListening.OnOff, forkArm.Off = forkArm.Stop, forkListening.Deactivate
}

// forkHearer decides overheard utterances.
var forkHearer = &listen.Hearer{
	State: forkListening,
	Client: func() (*openai.Client, string, bool) {
		t, ok := llm.ResolveOllama(llm.Load(), lang.Current(), forkKG(), listen.Model)
		return llm.NewClient(t), t.Model, ok
	},
	Say:      forkListenSay,
	Locked:   func(now time.Time) bool { _, l := quiet.Line(now); return l },
	Estonian: func() bool { return lang.Current() == lang.Estonian },
	LogDir:   os.Getenv("MYVECTOR_LISTEN_LOG"),
}

// forkListenSay speaks a listen reply in the saved T1 voice under behavior control, as sdkapp forkSay does.
func forkListenSay(esn, text string) error {
	robot, err := vars.GetRobot(esn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := voice.Control(ctx, robot)
	if err != nil {
		return err
	}
	defer release()
	_, err = voice.Speak(voice.WithFirstAudio(ctx, 7*time.Second), robot, text, voice.Current().For(lang.Current()))
	return err
}

// ForkTurnStart stamps a wake-word turn as its audio starts (preqs forkMicCapture), so a listen reply to the same words drops (H1).
func ForkTurnStart(esn string) { forkListening.Turn(esn, time.Now()) }

// forkListen runs right after forkQuiet (ProcessTextAll): it stamps a wake-word turn (A6), and an on/off phrase
// flips active listening and says so.
func forkListen(req interface{}, voiceText, botSerial string) bool {
	forkListening.Turn(botSerial, time.Now())
	on, ok := listen.Match(voiceText)
	if !ok {
		return false
	}
	text := forkListenFlip(botSerial, on)
	memory.MarkLLM(botSerial, voiceText) // ends req's turn as the LLM path does, LLM-marked so memory skips it
	IntentPass(req, "intent_greeting_hello", voiceText, map[string]string{}, false)
	forkConfirm(botSerial, text)
	return true
}

// forkKGSwitch is forkQuiet + forkListen for any knowledge-graph turn (follow-up, "I have a question",
// {{newVoiceRequest}}), which never reaches ProcessTextAll: a match is handled here and the LLM call is skipped.
// No wake word leads the turn, so "shut up" must match exactly, as overheard speech does (6e69164 ruling).
func forkKGSwitch(esn, text string) bool {
	if phrase, ok := quiet.MatchExact(text); ok {
		now := time.Now()
		forkQuietLock(esn, phrase, now)
		until := now.Add(quiet.LockFor)
		if l, err := quiet.Read(); err == nil && l.Until.After(until) {
			until = l.Until
		}
		// The nap lands under control, so the behavior tree re-selects sleep on release (no sleep intent on this path).
		if err := forkKGCut(esn, func() { forkNap(forkRobotIP(esn), esn, until) }); err != nil {
			logger.Println("my-vector quiet: kg cut: " + err.Error())
		}
		return true
	}
	on, ok := listen.Match(text)
	if !ok {
		return false
	}
	forkConfirm(esn, forkListenFlip(esn, on))
	return true
}

// forkKGCut takes behavior control, runs hold, and drops it, cutting streamingKG's placeholder answer as
// StreamingKGSim's BControl does (tests swap it).
var forkKGCut = func(esn string, hold func()) error {
	robot, err := vars.GetRobot(esn)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := voice.Control(ctx, robot)
	if err != nil {
		return err
	}
	hold()
	release()
	return nil
}

// forkListenFlip turns active listening on or off for esn and returns the confirmation to say.
func forkListenFlip(esn string, on bool) string {
	if on {
		forkListening.Activate(esn, time.Now())
		forkArm.Start(esn)
		logger.Println("myvector listen: on")
	} else {
		forkListening.Deactivate(esn, "phrase")
	}
	return listen.Confirm(on, lang.Current() == lang.Estonian)
}

// forkConfirmSay speaks the on/off confirmation (tests swap it).
var forkConfirmSay = forkListenSay

// ForkProbeMute keeps routeprobe off the robot: say replaces the confirmation speaker, and arming never sets its stream port.
func ForkProbeMute(say func(esn, text string) error) {
	forkConfirmSay = say
	forkArm.Set = func(string, int) error { return nil }
}

// forkConfirm says text in the PC voice (H5), opening an echo window over it.
func forkConfirm(esn, text string) {
	from := time.Now()
	err := forkConfirmSay(esn, text)
	forkListening.Spoken(esn, from, time.Now())
	if err != nil {
		logger.Println("myvector listen: say: " + err.Error())
	}
}

// forkResolve is llm.Resolve, listen.Model while any robot listens (model and client must agree); the saved profile is untouched.
func forkResolve(c llm.Config, l string, kg llm.KG) (llm.Target, bool) {
	if forkListening.Any(time.Now()) {
		return llm.ResolveOllama(c, l, kg, listen.Model)
	}
	return llm.Resolve(c, l, kg)
}
