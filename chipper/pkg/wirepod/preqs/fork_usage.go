package processreqs

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/usage"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
	"google.golang.org/grpc/peer"
)

// forkUsageLedger and forkUsageNow are seams for tests.
var (
	forkUsageLedger = usage.Open(vars.UsageLedgerPath)
	forkUsageNow    = time.Now
)

// forkUsageCount wraps the STT handler, outermost (server.go): ProcessIntent, ProcessIntentGraph and streamingKG each
// call sttHandler exactly once per robot request, so each speech stream counts once, after STT, as usage.Classify says
// (gRPC peer + transcript; an STT error counts with its empty text). The peer is read from the original stream, before
// forkMicCapture wraps it. A counting failure only logs.
func forkUsageCount(h func(sr.SpeechRequest) (string, error)) func(sr.SpeechRequest) (string, error) {
	if h == nil {
		return nil
	}
	return func(req sr.SpeechRequest) (string, error) {
		ip := forkPeerIP(req.Stream)
		text, err := h(req)
		if cerr := forkUsageLedger.Count(usage.Classify(ip, text), forkUsageNow()); cerr != nil {
			logger.Println("usage: " + cerr.Error())
		}
		return text, err
	}
}

// forkPeerIP is the gRPC peer IP of a chipper stream (it has Context()), nil when there is none.
func forkPeerIP(stream interface{}) (ip net.IP) {
	defer func() {
		if r := recover(); r != nil { // a stream value whose Context() panics: unknown peer, never a failed request
			logger.Println(fmt.Sprintf("usage: peer: %v", r))
			ip = nil
		}
	}()
	s, ok := stream.(interface{ Context() context.Context })
	if !ok {
		return nil
	}
	p, ok := peer.FromContext(s.Context())
	if !ok {
		return nil
	}
	return usage.AddrIP(p.Addr)
}
