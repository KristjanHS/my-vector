package wirepod_ttr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// ErrForkLLMOff: production sends intent_system_unmatched instead of asking the LLM (preqs/intent_graph.go's gate).
var ErrForkLLMOff = errors.New("LLM off")

// ForkProbeLLMResult is one ForkProbeLLMFull call: Finish and Usage from the non-streamed reply, Dur of the request.
type ForkProbeLLMResult struct {
	Target, Answer, Finish string
	Usage                  openai.Usage
	Dur                    time.Duration
}

// ForkProbeLLM asks the LLM what StreamingKGSim would for transcribedText, without the robot: same gate, client and
// request, one reply cleaned as it is spoken. target names who answers: "<provider> <model>" for the llm.Resolve
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
	var c *openai.Client
	switch k.Provider { // StreamingKGSim's switch
	case "together":
		if k.Model == "" {
			vars.APIConfig.Knowledge.Model = "meta-llama/Llama-3-70b-chat-hf" // StreamingKGSim also writes it to disk
		}
		conf := openai.DefaultConfig(k.Key)
		conf.BaseURL = "https://api.together.xyz/v1"
		c = openai.NewClientWithConfig(conf)
	case "custom":
		conf := openai.DefaultConfig(k.Key)
		conf.BaseURL = k.Endpoint
		c = openai.NewClientWithConfig(conf)
	case "openai":
		c = openai.NewClient(k.Key)
	}
	if t, ok := forkResolve(llm.Load(), lang.Current(), forkKG()); ok {
		res.Target = t.Provider + " " + t.Model
	} else if c == nil {
		return res, fmt.Errorf("no client for Knowledge provider %q and no fork profile for %s", k.Provider, lang.Current())
	} else {
		res.Target = "wire-pod knowledge " + k.Provider + " " + vars.APIConfig.Knowledge.Model
	}
	c = forkClient(c)
	r := CreateAIReq(transcribedText, esn, false, false)
	r.Stream = false
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
	res.Answer = removeSpecialCharacters(resp.Choices[0].Message.Content)
	if resp.Choices[0].FinishReason == openai.FinishReasonLength {
		res.Answer = forkCutTail(res.Answer, true) // as the stream loops' forkDropCut
	}
	return res, nil
}
