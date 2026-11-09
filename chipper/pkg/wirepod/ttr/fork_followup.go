package wirepod_ttr

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/followup"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/vars"
	openai "github.com/sashabaranov/go-openai"
)

var (
	forkDecideTimeout = followup.Timeout // bounds one decider call (tests shorten it)
	forkFollowUpWait  = followup.Timeout // forkFollowUp's wait for a pending decision (tests shorten it)
	forkChain         followup.Chain
	forkDecider       = forkDecide
	forkFire          = func(robot *vector.Vector) { go DoNewRequest(robot) }
)

// forkTurn carries one request's decider channel (nil = no decider) from its stream goroutine to its speak loop.
type forkTurn chan (<-chan bool)

// forkDecide asks t's brain whether the robot should listen again after reply (F1); error or timeout → false.
func forkDecide(t llm.Target, ask, reply string) <-chan bool {
	ch := make(chan bool, 1)
	go func() { ch <- forkDecideNow(t, ask, reply) }()
	return ch
}

// forkAfterReply runs at StreamingKGSim's stream-EOF Remember site: without SaveChat it still remembers the turn (F3),
// and it starts the follow-up decider on the answering brain while the robot speaks (F1).
func forkAfterReply(turn forkTurn, esn string, isKG bool, ask, reply string) {
	if !vars.APIConfig.Knowledge.SaveChat {
		Remember(openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: ask},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: reply}, esn)
	}
	var d <-chan bool
	if t, ok := forkResolve(llm.Load(), lang.Current(), forkKG()); ok {
		d = forkDecider(t, ask, reply)
	}
	turn <- d
}

// forkBegin runs as StreamingKGSim starts: a knowledge request consumes an armed follow-up (F2), any other
// request resets the chain; a blank one, an armed one STT scored as no speech, or a quiet/listen phrase
// (forkKGSwitch) ends it and skips the LLM call (B1). turn is this request's forkTurn.
func forkBegin(esn string, isKG bool, text string) (turn forkTurn, skip bool) {
	turn = make(forkTurn, 1)
	p := followup.TakeNoSpeech(esn)
	switch {
	case !isKG:
		forkChain.Reset(esn)
	case strings.TrimSpace(text) == "":
		forkChain.Reset(esn)
		logger.Println("myvector: follow-up blank, chain ends")
		return turn, true
	case forkKGSwitch(esn, text):
		forkChain.Reset(esn)
		logger.Println("myvector: follow-up switch phrase, chain ends")
		return turn, true
	default:
		if forkChain.Begin(esn, forkNow()) && p >= followup.NoSpeech {
			forkChain.Reset(esn)
			logger.Println("myvector: stt no-speech " + strconv.FormatFloat(p, 'f', 3, 64) + ", blank, chain ends")
			return turn, true
		}
	}
	return turn, false
}

// forkFollowUp runs after the reply is spoken: on the decider's YES under the chain cap it arms the chain and
// re-opens the mic (F1, F2); skip (interrupted / disconnected) drops the decision. Waits up to forkFollowUpWait
// for turn's decider and its answer. Reports whether it fired.
func forkFollowUp(robot *vector.Vector, turn forkTurn, esn string, skip bool) bool {
	if skip {
		return false
	}
	max := forkMax(esn)
	if !forkChain.Allow(esn, max) {
		logger.Println("myvector: follow-up " + strconv.Itoa(max) + "/" + strconv.Itoa(max) + " cap reached, chain ends")
		return false
	}
	n := strconv.Itoa(forkChain.Count(esn)+1) + "/" + strconv.Itoa(max)
	verdict, yes := "timeout", false
	deadline := time.After(forkFollowUpWait)
	select {
	case d := <-turn:
		if d == nil {
			return false
		}
		select {
		case yes = <-d:
			verdict = map[bool]string{true: "YES", false: "NO"}[yes]
		case <-deadline:
		}
	case <-deadline:
	}
	logger.Println("myvector: follow-up " + n + " " + verdict)
	if yes {
		forkChain.Arm(esn, forkNow())
		forkFire(robot)
	}
	return yes
}

// forkRepeats is one reply's G2 repeat filter, holding esn's remembered assistant messages from the stream's start.
type forkRepeats struct {
	esn     string
	said    []string
	dropped int
}

func forkRepeatFilter(esn string) *forkRepeats {
	r := &forkRepeats{esn: esn}
	for _, m := range GetChat(esn).Chats {
		if m.Role == openai.ChatMessageRoleAssistant {
			r.said = append(r.said, m.Content)
		}
	}
	return r
}

// drop: sentence repeats a remembered question (followup.Repeats) and is not spoken; full becomes the kept chunks +
// rest, so upstream's "content after the last punctuation mark" check adds nothing back (forkDropCut's move).
func (r *forkRepeats) drop(sentence, rest string, chunks []string, full *string) bool {
	if !followup.Repeats(sentence, r.said, followup.RepeatMin) {
		return false
	}
	r.dropped++
	// rest keeps forkSplitSentence's leading space: untrimmed it doubled the join's space, full stopped matching the
	// kept chunks joined, and upstream's EOF check appended an empty chunk
	*full = strings.TrimSpace(strings.Join(append(chunks[:len(chunks):len(chunks)], strings.TrimSpace(rest)), " "))
	logger.Println("myvector: follow-up repeat dropped: " + sentence)
	return true
}

// only runs at stream EOF: every sentence was a dropped repeat, so the reply says nothing and the chain ends.
func (r *forkRepeats) only(turn forkTurn, chunks []string) bool {
	if len(chunks) > 0 || r.dropped == 0 {
		return false
	}
	forkChain.Reset(r.esn)
	turn <- nil
	logger.Println("myvector: reply was only repeated questions, says nothing, chain ends")
	return true
}

// wake runs at stream EOF after isDone is set and the last chunk appended: a dropped sentence sends no speakReady
// wake, so a speak loop that spoke every kept chunk and waits for the next one would never see isDone. Bounded like
// forkSignalHeld (forkHeldWait, in a goroutine), so it never blocks the stream goroutine; a loop still speaking finds
// isDone by itself and the send times out.
func (r *forkRepeats) wake(speakReady chan string) {
	if r.dropped == 0 {
		return
	}
	timeout := time.After(forkHeldWait)
	go func() {
		select {
		case speakReady <- "":
		case <-timeout:
		}
	}()
}

// forkNextChunk is the speak loop's chunk n: chunks()[n] once the stream has it, ok false once done() and none is
// left. It waits on speakReady and re-checks after every wake, so a wake that brings no chunk (rep.wake, a stale
// forkSignalHeld) neither indexes past the slice nor stops the loop early. chunks and done read the stream
// goroutine's fullRespSlice and isDone.
func forkNextChunk(chunks func() []string, done func() bool, n int, speakReady chan string) (string, bool) {
	for {
		if s := chunks(); n < len(s) {
			return s[n], true
		}
		if done() {
			return "", false
		}
		logger.Println("Waiting for more content from LLM...")
		<-speakReady
	}
}

// forkMax is esn's chain cap: MaxListening while active listening is on (G1), else Max.
func forkMax(esn string) int {
	if forkListening.Active(esn, forkNow()) {
		return followup.MaxListening
	}
	return followup.Max
}

func forkDecideNow(t llm.Target, ask, reply string) bool {
	r := openai.ChatCompletionRequest{Model: t.Model, Messages: []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: followup.Prompt},
		{Role: openai.ChatMessageRoleUser, Content: followup.User(ask, reply)}}}
	if t.Ollama() {
		r.MaxTokens = followup.MaxTokens
		llm.Load().Thinking(llm.Ollama, t.Model, llm.PathFollowUp).Apply(&r, followup.MaxTokens)
	} else {
		r.Model = followup.ClaudeModel
		llm.SamplingFor(t.Provider, r.Model).Apply(&r)
		r.MaxCompletionTokens = followup.MaxTokens
	}
	ctx, cancel := context.WithTimeout(context.Background(), forkDecideTimeout)
	defer cancel()
	res, err := llm.NewClientNoThink(t).CreateChatCompletion(ctx, r)
	if err != nil || len(res.Choices) == 0 {
		logger.Println("myvector: follow-up decider:", r.Model, err)
		return false
	}
	return followup.Parse(res.Choices[0].Message.Content)
}
