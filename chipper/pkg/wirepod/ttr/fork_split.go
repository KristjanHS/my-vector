package wirepod_ttr

import (
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/logger"
)

// forkSplitSentence cuts the first finished sentence off the LLM stream buffer (both stream loops
// in kgsim.go and kgsim_cmds.go). Upstream split at the first '.' of any kind, so "kell 10.10" or
// "3.14" became two spoken sentences ("kell 10." … "10."). Here a '.' between two digits is not a
// sentence end, and a '.' after a digit at the end of the buffer waits for the next chunk, which
// may bring the rest of the number. A sentence ends at ... .' ." . ? or !, and everything after it
// stays in rest (upstream dropped text past a second separator in the same buffer); the loops
// call it until no sentence is left, so a chunk with several sentences releases them all.
func forkSplitSentence(buf string) (sentence, rest string, ok bool) {
	s := strings.TrimSpace(buf)
	for i := 0; i < len(s); i++ {
		end := 0
		switch s[i] {
		case '?', '!':
			end = i + 1
		case '.':
			switch {
			case strings.HasPrefix(s[i:], "..."):
				end = i + 3
			case i > 0 && forkIsDigit(s[i-1]) && (i+1 == len(s) || forkIsDigit(s[i+1])):
				continue // 10.10, 3.14 — or a number still streaming in
			case i+1 < len(s) && (s[i+1] == '\'' || s[i+1] == '"'):
				end = i + 2
			default:
				end = i + 1
			}
		default:
			continue
		}
		return strings.TrimSpace(s[:end]), s[end:], true
	}
	return "", buf, false
}

// forkFlushHeld: at the end of the stream, text still in the buffer (a sentence held by
// forkSplitSentence, e.g. one ending in "2026.") becomes the last chunk before the reply is
// logged and remembered; held is that chunk, for forkSignalHeld. With no chunks yet,
// forkFlushUnpunctuated handles it instead.
func forkFlushHeld(chunks []string, tail string) (_ []string, _ string, held string) {
	if len(chunks) == 0 || strings.TrimSpace(tail) == "" {
		return chunks, tail, ""
	}
	held = strings.TrimSpace(tail)
	return append(chunks, held), "", held
}

// forkCutTail is tail without its unfinished last sentence, for a reply the reply cap cut off
// (finish_reason "length"). A '.' after a digit at the very end counts as a sentence end: no more
// text is coming. With none (no sentence spoken yet) a tail with no finished sentence stays whole:
// a fragment beats "no response".
func forkCutTail(tail string, none bool) string {
	var kept []string
	buf := tail
	for s, r, ok := forkSplitSentence(buf); ok; s, r, ok = forkSplitSentence(buf) {
		kept, buf = append(kept, s), r
	}
	if rest := strings.TrimSpace(buf); strings.HasSuffix(rest, ".") {
		kept = append(kept, rest) // held by forkSplitSentence at a digit '.'
	}
	if len(kept) == 0 && none {
		return strings.TrimSpace(tail)
	}
	return strings.Join(kept, " ")
}

// forkDropCut runs at the end of a stream (both loops) before forkFlushHeld: when the cap cut the
// reply, the tail keeps only its finished sentences and full becomes the text that will be spoken,
// so upstream's "content after the last punctuation mark" check adds nothing back.
func forkDropCut(cut bool, chunks []string, full, tail *string) {
	forkDropCutIf(cut, len(chunks) == 0, chunks, full, tail)
}

func forkDropCutIf(cut, none bool, chunks []string, full, tail *string) {
	if !cut {
		return
	}
	*tail = forkCutTail(*tail, none)
	*full = strings.TrimSpace(strings.Join(append(chunks[:len(chunks):len(chunks)], *tail), " "))
	logger.Println("myvector llm: reply hit the cap; unfinished last sentence dropped")
}

// forkHeldWait bounds forkSignalHeld's send.
var forkHeldWait = time.Second

// forkSignalHeld wakes a speak loop that already spoke every chunk and waits on speakReady:
// without it the held last sentence is never spoken and the loop blocks forever. Call it after
// fullRespSlice holds the chunk. The send is in a goroutine with a timeout, so it never blocks
// the stream goroutine; a loop still speaking finds the chunk in the slice by itself.
func forkSignalHeld(speakReady chan string, held string) {
	if held == "" {
		return
	}
	timeout := time.After(forkHeldWait)
	go func() {
		select {
		case speakReady <- held:
		case <-timeout:
		}
	}()
}

func forkIsDigit(c byte) bool { return c >= '0' && c <= '9' }
