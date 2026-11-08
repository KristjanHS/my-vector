package wirepod_ttr

import "strings"

// forkFlushUnpunctuated: a stream that ends before any . ? ! (e.g. a bare
// "{{getImage||front}}") becomes one chunk instead of "llm returned no response".
func forkFlushUnpunctuated(chunks []string, tail string) ([]string, bool) {
	if len(chunks) > 0 || strings.TrimSpace(tail) == "" {
		return chunks, false
	}
	return []string{strings.TrimSpace(tail)}, true
}
