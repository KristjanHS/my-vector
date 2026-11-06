package wirepod_ttr

import (
	"context"

	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	"github.com/sashabaranov/go-openai"
)

// ForkProbeLLM asks the LLM what StreamingKGSim would for transcribedText, without the robot: same client + request, one reply.
func ForkProbeLLM(ctx context.Context, transcribedText, esn string) (string, error) {
	var c *openai.Client
	switch vars.APIConfig.Knowledge.Provider { // StreamingKGSim's switch
	case "together":
		conf := openai.DefaultConfig(vars.APIConfig.Knowledge.Key)
		conf.BaseURL = "https://api.together.xyz/v1"
		c = openai.NewClientWithConfig(conf)
	case "custom":
		conf := openai.DefaultConfig(vars.APIConfig.Knowledge.Key)
		conf.BaseURL = vars.APIConfig.Knowledge.Endpoint
		c = openai.NewClientWithConfig(conf)
	default:
		c = openai.NewClient(vars.APIConfig.Knowledge.Key)
	}
	c = forkClient(c)
	r := CreateAIReq(transcribedText, esn, false, false)
	r.Stream = false
	resp, err := c.CreateChatCompletion(ctx, r)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", nil
	}
	return resp.Choices[0].Message.Content, nil
}
