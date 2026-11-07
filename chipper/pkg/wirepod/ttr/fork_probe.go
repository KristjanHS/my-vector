package wirepod_ttr

import (
	"context"
	"errors"
	"fmt"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// ErrForkLLMOff: production sends intent_system_unmatched instead of asking the LLM (preqs/intent_graph.go's gate).
var ErrForkLLMOff = errors.New("LLM off")

// ForkProbeLLM asks the LLM what StreamingKGSim would for transcribedText, without the robot: same gate, client and
// request, one reply cleaned as it is spoken. target names who answers: "<provider> <model>" for the llm.Resolve
// target, else "wire-pod knowledge <provider> <model>".
func ForkProbeLLM(ctx context.Context, transcribedText, esn string) (target, answer string, err error) {
	k := vars.APIConfig.Knowledge
	if !k.IntentGraph || !k.Enable {
		return "", "", fmt.Errorf("%w: knowledge enable=%v intentgraph=%v", ErrForkLLMOff, k.Enable, k.IntentGraph)
	}
	if k.Provider == "houndify" {
		return "", "", fmt.Errorf("%w: provider houndify", ErrForkLLMOff)
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
	if t, ok := llm.Resolve(llm.Load(), lang.Current(), forkKG()); ok {
		target = t.Provider + " " + t.Model
	} else if c == nil {
		return "", "", fmt.Errorf("no client for Knowledge provider %q and no fork profile for %s", k.Provider, lang.Current())
	} else {
		target = "wire-pod knowledge " + k.Provider + " " + vars.APIConfig.Knowledge.Model
	}
	c = forkClient(c)
	r := CreateAIReq(transcribedText, esn, false, false)
	r.Stream = false
	resp, err := c.CreateChatCompletion(ctx, r)
	if err != nil {
		return target, "", err
	}
	if len(resp.Choices) == 0 {
		return target, "", nil
	}
	answer = removeSpecialCharacters(resp.Choices[0].Message.Content)
	if resp.Choices[0].FinishReason == openai.FinishReasonLength {
		answer = forkCutTail(answer, true) // as the stream loops' forkDropCut
	}
	return target, answer, nil
}
