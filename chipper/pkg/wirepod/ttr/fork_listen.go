package wirepod_ttr

import (
	"context"
	"net/http"
	"os"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
	openai "github.com/sashabaranov/go-openai"
)

var forkListening = func() *listen.State {
	s := listen.NewState()
	s.Log = func(l string) { logger.Println(l) }
	return s
}()

// forkCapture is the old listen.sh clip path; nothing Starts it since the stream (M6), Stage 3.2 deletes it.
var forkCapture = &listen.Runner{
	Script: listen.ScriptPath,
	Log:    func(l string) { logger.Println(l) },
}

// forkJoiner turns listen.sh clips (listen.ClipAPI) into utterances for forkHearer.
var forkJoiner = &listen.Joiner{
	STT:     func(pcm []byte) (string, error) { return listen.STT(lang.Current(), pcm) },
	Heard:   func(u listen.Utterance) { go forkHearer.Heard(u) },
	Clipped: func(esn string) { forkCapture.Clipped(esn) },
	Log:     func(l string) { logger.Println(l) },
}

func init() {
	forkListening.OnOff, forkArm.Off = forkArm.Stop, forkListening.Deactivate
	http.HandleFunc(listen.ClipAPI, listen.ClipHandler(forkListening, forkJoiner, func(l string) { logger.Println(l) }))
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
	Quiet:    func(esn, phrase string) { forkQuietLock(esn, phrase, time.Now()) },
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
	text := listen.Confirm(on, lang.Current() == lang.Estonian)
	if on {
		forkListening.Activate(botSerial, time.Now())
		forkArm.Start(botSerial)
		logger.Println("myvector listen: on")
	} else {
		forkListening.Deactivate(botSerial, "phrase")
	}
	forkSpeak(req, botSerial, voiceText, text)
	return true
}

// forkSpeak says text as the reply to req (the plugin-response path).
func forkSpeak(req interface{}, esn, voiceText, text string) {
	if igr, ok := req.(*vtt.IntentGraphRequest); ok {
		igr.Stream.Send(&pb.IntentGraphResponse{Session: igr.Session, DeviceId: igr.Device,
			ResponseType: pb.IntentGraphMode_KNOWLEDGE_GRAPH, SpokenText: text, QueryText: voiceText, IsFinal: true})
		return
	}
	if err := KGSim(esn, text); err != nil {
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
