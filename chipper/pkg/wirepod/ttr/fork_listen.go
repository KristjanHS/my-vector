package wirepod_ttr

import (
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/listen"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vtt"
)

var forkListening = func() *listen.State {
	s := listen.NewState()
	s.Log = func(l string) { logger.Println(l) }
	return s
}()

// forkListen runs right after forkQuiet (ProcessTextAll): an on/off phrase flips active listening and says so.
func forkListen(req interface{}, voiceText, botSerial string) bool {
	on, ok := listen.Match(voiceText)
	if !ok {
		return false
	}
	text := map[bool]map[bool]string{
		false: {true: "Active listening on.", false: "Active listening off."},
		true:  {true: "Aktiivne kuulamine sees.", false: "Aktiivne kuulamine väljas."},
	}[lang.Current() == lang.Estonian][on]
	if on {
		forkListening.Activate(botSerial, time.Now())
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
