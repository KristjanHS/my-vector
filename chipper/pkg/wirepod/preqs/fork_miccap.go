package processreqs

import (
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/miccap"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
)

// forkMicCapture wraps the STT handler so each request's audio lands in miccap.Dir() while that dir exists.
func forkMicCapture(h func(sr.SpeechRequest) (string, error)) func(sr.SpeechRequest) (string, error) {
	if h == nil {
		return nil
	}
	return func(req sr.SpeechRequest) (string, error) {
		dir := miccap.Dir()
		if dir == "" {
			return h(req)
		}
		var s miccap.Sink
		s.Write(req.MicData) // raw first chunk; FirstReq is already decoded+filtered when opus
		req.Stream = s.Wrap(req.Stream)
		text, err := h(req)
		if p, serr := s.Save(dir, text, req.IsOpus, time.Now()); serr != nil {
			logger.Println("miccap: " + serr.Error())
		} else {
			logger.Println("miccap: saved " + p)
		}
		return text, err
	}
}
