package wirepod_ttr

import "strings"

// forkSplitSentence cuts the first finished sentence off the LLM stream buffer (both stream loops
// in kgsim.go and kgsim_cmds.go). Upstream split at the first '.' of any kind, so "kell 10.10" or
// "3.14" became two spoken sentences ("kell 10." … "10."). Here a '.' between two digits is not a
// sentence end, and a '.' after a digit at the end of the buffer waits for the next chunk, which
// may bring the rest of the number. A sentence ends at ... .' ." . ? or !, and everything after it
// stays in rest (upstream dropped text past a second separator in the same buffer).
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
// logged and remembered. With no chunks yet, forkFlushUnpunctuated handles it instead.
func forkFlushHeld(chunks []string, tail string) ([]string, string) {
	if len(chunks) == 0 || strings.TrimSpace(tail) == "" {
		return chunks, tail
	}
	return append(chunks, strings.TrimSpace(tail)), ""
}

func forkIsDigit(c byte) bool { return c >= '0' && c <= '9' }
