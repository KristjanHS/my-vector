package wirepod_ttr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// ErrForkLLMOff: production sends intent_system_unmatched instead of asking the LLM (preqs/intent_graph.go's gate).
var ErrForkLLMOff = errors.New("LLM off")

// ForkProbeLLMResult is one ForkProbeLLMFull call: Finish and Usage from the non-streamed reply, Dur of the request.
// RawAnswer is the content before cleaning; Reasoning is Ollama's "reasoning" (go-openai drops it); BaseURL is the
// llm.Resolve target's ("" for wire-pod knowledge).
type ForkProbeLLMResult struct {
	Target, BaseURL, Answer, RawAnswer, Reasoning, Finish string
	Usage                                                 openai.Usage
	Dur                                                   time.Duration
}

// forkProbeBody is the probe client's transport: it keeps the last response body for ForkProbeLLMFull to decode.
type forkProbeBody struct{ raw []byte }

func (b *forkProbeBody) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	b.raw, err = io.ReadAll(res.Body)
	res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b.raw))
	return res, err
}

// ForkProbeLLM asks the LLM what StreamingKGSim would for transcribedText, without the robot: same gate, client and
// request, one reply cleaned and kept as it is spoken and remembered (forkProbeSpoken). target names who answers: "<provider> <model>" for the llm.Resolve
// target, else "wire-pod knowledge <provider> <model>".
func ForkProbeLLM(ctx context.Context, transcribedText, esn string) (target, answer string, err error) {
	r, err := ForkProbeLLMFull(ctx, transcribedText, esn)
	return r.Target, r.Answer, err
}

// ForkProbeLLMFull is ForkProbeLLM with the reply's finish_reason, usage and duration.
func ForkProbeLLMFull(ctx context.Context, transcribedText, esn string) (ForkProbeLLMResult, error) {
	var res ForkProbeLLMResult
	k := vars.APIConfig.Knowledge
	if !k.IntentGraph || !k.Enable {
		return res, fmt.Errorf("%w: knowledge enable=%v intentgraph=%v", ErrForkLLMOff, k.Enable, k.IntentGraph)
	}
	if k.Provider == "houndify" {
		return res, fmt.Errorf("%w: provider houndify", ErrForkLLMOff)
	}
	body := &forkProbeBody{}
	client := func(key, base string) *openai.Client { // StreamingKGSim's / forkClient's config, body-keeping transport
		conf := openai.DefaultConfig(key)
		if base != "" {
			conf.BaseURL = base
		}
		conf.HTTPClient = &http.Client{Transport: body}
		return openai.NewClientWithConfig(conf)
	}
	var c *openai.Client
	switch k.Provider { // StreamingKGSim's switch
	case "together":
		if k.Model == "" {
			vars.APIConfig.Knowledge.Model = "meta-llama/Llama-3-70b-chat-hf" // StreamingKGSim also writes it to disk
		}
		c = client(k.Key, "https://api.together.xyz/v1")
	case "custom":
		c = client(k.Key, k.Endpoint)
	case "openai":
		c = client(k.Key, "")
	}
	if t, ok := forkResolve(llm.Load(), lang.Current(), forkKG()); ok {
		res.Target = t.Provider + " " + t.Model
		if t.Ollama() {
			res.BaseURL = t.BaseURL
		}
		c = client(t.Key, t.BaseURL) // forkClient's llm.NewClient(t)
	} else if c == nil {
		return res, fmt.Errorf("no client for Knowledge provider %q and no fork profile for %s", k.Provider, lang.Current())
	} else {
		res.Target = "wire-pod knowledge " + k.Provider + " " + vars.APIConfig.Knowledge.Model
	}
	r := CreateAIReq(transcribedText, esn, false, false)
	r.Stream = false
	rep := forkRepeatFilter(esn) // the stream goroutine's G2 snapshot: the chat before this turn
	start := time.Now()
	resp, err := c.CreateChatCompletion(ctx, r)
	res.Dur = time.Since(start)
	if err != nil {
		return res, err
	}
	res.Usage = resp.Usage
	if len(resp.Choices) == 0 {
		return res, nil
	}
	res.Finish = string(resp.Choices[0].FinishReason)
	res.RawAnswer = resp.Choices[0].Message.Content
	res.Reasoning = resp.Choices[0].Message.ReasoningContent
	var raw struct {
		Choices []struct {
			Message struct {
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body.raw, &raw) == nil && len(raw.Choices) > 0 && raw.Choices[0].Message.Reasoning != "" {
		res.Reasoning = raw.Choices[0].Message.Reasoning
	}
	res.Answer = forkProbeSpoken(removeSpecialCharacters(res.RawAnswer), resp.Choices[0].FinishReason == openai.FinishReasonLength, rep)
	return res, nil
}

// forkProbeSpoken is a whole reply as StreamingKGSim's stream loop keeps it — what is spoken and what forkAfterReply
// remembers: the forkSplitSentence split with rep's G2 repeat drops, then the stream EOF's rep.dropCut (cut = the
// cap cut the reply), forkFlushHeld and forkFlushUnpunctuated, the kept chunks joined. "" = nothing is spoken.
func forkProbeSpoken(answer string, cut bool, rep *forkRepeats) string {
	var chunks []string
	full, tail := answer, answer
	for s, rest, ok := forkSplitSentence(tail); ok; s, rest, ok = forkSplitSentence(tail) {
		if !rep.drop(s, rest, chunks, &full) {
			chunks = append(chunks, s)
		}
		tail = rest
	}
	rep.dropCut(cut, chunks, &full, &tail)
	chunks, tail, _ = forkFlushHeld(chunks, tail)
	chunks, _ = forkFlushUnpunctuated(chunks, tail)
	return strings.Join(chunks, " ")
}
