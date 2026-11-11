package wirepod_ttr

import (
	"context"
	"io"
	"sync"
	"time"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	openai "github.com/sashabaranov/go-openai"
)

// forkCanned is esn's forkCannedReply that replaces its next LLM stream for the same ask (N11 templated et weather);
// every request build sets or clears it.
var forkCanned sync.Map

type forkCannedReply struct{ ask, text string }

var forkWeatherTemplate = search.WeatherReply

// forkCannedSet gives an OllamaEt Weather ask its search.WeatherReply; any other ask clears esn's.
func forkCannedSet(esn, last, l, brain string) {
	if brain == prompt.OllamaEt {
		if r, ok := forkWeatherTemplate(last, l, forkWeatherInessive, forkBotZone(esn)); ok {
			forkCanned.Store(esn, forkCannedReply{last, r})
			return
		}
	}
	forkCanned.Delete(esn)
}

// forkWeatherInessive is name's et "-s" form from forkWeatherExonyms, "" when it has none.
func forkWeatherInessive(name string) string {
	for in, n := range forkWeatherExonyms {
		if n == name {
			return capitalFirst(in)
		}
	}
	return ""
}

func capitalFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

type forkRecv interface {
	Recv() (openai.ChatCompletionStreamResponse, error)
}

// forkLLMStream is esn's canned reply as a one-chunk stream when it was built for req's ask, else c's LLM stream
// (after esn's prefilled opener when one was built for req's ask, fork_prefill.go).
func forkLLMStream(c *openai.Client, ctx context.Context, req openai.ChatCompletionRequest, esn string) (forkRecv, error) {
	if r, ok := forkCannedStreamFor(req, esn); ok {
		logger.Println("myvector: weather template reply, no LLM")
		return r, nil
	}
	req, opener, pre := forkPrefillFor(req, esn)
	s, err := forkOpenStream(c, ctx, req)
	if err != nil {
		return nil, err
	}
	if pre {
		logger.Println("myvector: reply prefill", opener)
		return &forkPrefixStream{s: s, text: opener}, nil
	}
	return s, nil
}

// forkOpenStream opens c's LLM stream; a var so tests fake it.
var forkOpenStream = func(c *openai.Client, ctx context.Context, req openai.ChatCompletionRequest) (forkRecv, error) {
	s, err := c.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// forkCannedStreamFor takes esn's canned reply when it was built for req's last message. Another ask's entry is left
// alone: an overlapping request for esn may just have stored it, and the next request build sets or clears it anyway.
func forkCannedStreamFor(req openai.ChatCompletionRequest, esn string) (*forkCannedStream, bool) {
	v, ok := forkCanned.Load(esn)
	if !ok {
		return nil, false
	}
	r := v.(forkCannedReply)
	if n := len(req.Messages); n == 0 || req.Messages[n-1].Content != r.ask || !forkCanned.CompareAndDelete(esn, v) {
		return nil, false
	}
	return &forkCannedStream{text: r.text}, true
}

type forkCannedStream struct {
	text string
	sent bool
}

func (s *forkCannedStream) Recv() (openai.ChatCompletionStreamResponse, error) {
	if s.sent {
		return openai.ChatCompletionStreamResponse{}, io.EOF
	}
	s.sent = true
	return openai.ChatCompletionStreamResponse{Created: time.Now().Unix(), Choices: []openai.ChatCompletionStreamChoice{{
		Delta: openai.ChatCompletionStreamChoiceDelta{Content: s.text}, FinishReason: openai.FinishReasonStop}}}, nil
}

// forkIntentWait bounds forkSignalIntent's send: StreamingKGSim reads successIntent once, so a late send just expires.
var forkIntentWait = 5 * time.Second

// forkSignalIntent sends successIntent's true from a goroutine once the stream ended with chunks, in case every split
// send ran before StreamingKGSim ranged successIntent.
func forkSignalIntent(c chan bool) {
	go func() {
		select {
		case c <- true:
		case <-time.After(forkIntentWait):
		}
	}()
}
