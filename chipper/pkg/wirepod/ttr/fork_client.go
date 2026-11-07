package wirepod_ttr

import (
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/sashabaranov/go-openai"
)

// forkClient is the client for the current language's forkResolve target, else c (wire-pod's Knowledge one).
func forkClient(c *openai.Client) *openai.Client {
	l := lang.Current()
	t, ok := forkResolve(llm.Load(), l, forkKG())
	if !ok {
		return c
	}
	logger.Println("myvector llm: " + l + " → " + t.Provider + " " + t.Model + " @ " + t.BaseURL)
	return llm.NewClient(t)
}
