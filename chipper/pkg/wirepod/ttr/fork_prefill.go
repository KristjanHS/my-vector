package wirepod_ttr

import (
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/search"
	openai "github.com/sashabaranov/go-openai"
)

// forkNewsOpener is the OllamaEt reply's prefilled opener on a news ask with web results (search-news-quality Q9/Q10).
const forkNewsOpener = "Värskete uudiste järgi"

// forkPrefill holds esn's opener for its ask, set by forkAIReq after the search and taken by forkLLMStream.
var forkPrefill sync.Map // esn -> forkCannedReply{ask, opener}

// forkPrefillSet gives an OllamaEt news ask with a web block (not Unavailable) its opener; any other ask clears esn's.
func forkPrefillSet(esn, last, l, brain, block string, prev []string) {
	if brain == prompt.OllamaEt && block != "" && block != search.Unavailable(l) && search.NewsAsk(last, prev, l) {
		forkPrefill.Store(esn, forkCannedReply{last, forkNewsOpener})
		return
	}
	forkPrefill.Delete(esn)
}

// forkPrefillFor takes esn's opener when it was built for req's last message: req gains it as a trailing assistant
// message (a copy; the caller's chat history keeps no prefill) and the stream speaks it ahead of the continuation.
func forkPrefillFor(req openai.ChatCompletionRequest, esn string) (openai.ChatCompletionRequest, string, bool) {
	v, ok := forkPrefill.Load(esn)
	if !ok {
		return req, "", false
	}
	r := v.(forkCannedReply)
	if n := len(req.Messages); n == 0 || req.Messages[n-1].Content != r.ask || !forkPrefill.CompareAndDelete(esn, v) {
		return req, "", false
	}
	req.Messages = append(slices.Clone(req.Messages), openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: r.text})
	return req, r.text, true
}

// forkPrefixStream prepends text to s's first non-empty content delta (Ollama's deltas omit the prefill), joined by a
// space unless the delta starts with a space or punctuation.
type forkPrefixStream struct {
	s    forkRecv
	text string
	done bool
}

func (p *forkPrefixStream) Recv() (openai.ChatCompletionStreamResponse, error) {
	r, err := p.s.Recv()
	if err != nil || p.done || len(r.Choices) == 0 || r.Choices[0].Delta.Content == "" {
		return r, err
	}
	p.done = true
	r.Choices[0].Delta.Content = forkJoinOpener(p.text, r.Choices[0].Delta.Content)
	return r, nil
}

// forkJoinOpener is opener then c, space-joined unless c starts with a space or closing punctuation.
func forkJoinOpener(opener, c string) string {
	if c == "" || unicode.IsSpace([]rune(c)[0]) || strings.ContainsRune(",.;:!?)…»”\"", []rune(c)[0]) {
		return opener + c
	}
	return opener + " " + c
}
