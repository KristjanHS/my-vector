package wirepod_ttr

import (
	"reflect"
	"testing"
)

func TestForkSplitSentence(t *testing.T) {
	for _, c := range []struct{ buf, sentence, rest string }{
		{"Kell on 10.10. Ilm", "Kell on 10.10.", " Ilm"},
		{"Pii on 3.14! Jah", "Pii on 3.14!", " Jah"},
		{"Tere. Kuidas läheb? Hästi", "Tere.", " Kuidas läheb? Hästi"}, // the rest is kept, not dropped
		{"Oota... nüüd", "Oota...", " nüüd"},
		{`Ta ütles "jah." Siis`, `Ta ütles "jah."`, " Siis"},
		{"Mis kell on?", "Mis kell on?", ""},
		{"Aasta on 2026. Nüüd", "Aasta on 2026.", " Nüüd"}, // digit then space: a sentence end
	} {
		s, r, ok := forkSplitSentence(c.buf)
		if !ok || s != c.sentence || r != c.rest {
			t.Errorf("forkSplitSentence(%q) = %q, %q, %v; want %q, %q", c.buf, s, r, ok, c.sentence, c.rest)
		}
	}
	for _, buf := range []string{"Kell on 10.", "Kell on 10.1", "Pii on 3", "  "} {
		if s, r, ok := forkSplitSentence(buf); ok {
			t.Errorf("forkSplitSentence(%q) = %q, %q, true; want held", buf, s, r)
		}
	}
}

// forkStream runs chunks through the stream loop's steps: one split per chunk, then the EOF flush.
func forkStream(chunks ...string) []string {
	var out []string
	buf := ""
	for _, c := range chunks {
		buf += c
		if s, r, ok := forkSplitSentence(buf); ok {
			out, buf = append(out, s), r
		}
	}
	out, buf = forkFlushHeld(out, buf)
	out, _ = forkFlushUnpunctuated(out, buf)
	return out
}

func TestForkStreamKeepsNumbers(t *testing.T) {
	for _, c := range []struct {
		chunks []string
		want   []string
	}{
		{[]string{"Kell", " on", " 10", ".", "10", ".", " Ilus", " päev", "."}, []string{"Kell on 10.10.", "Ilus päev."}},
		{[]string{"Pii on 3", ".", "14", " ehk", " nii", ". Jah", "."}, []string{"Pii on 3.14 ehk nii.", "Jah."}},
		{[]string{"Tere", ".", " Aasta on 2026", "."}, []string{"Tere.", "Aasta on 2026."}}, // held to EOF, then flushed
		{[]string{"Kell on 10", ".", "10", "."}, []string{"Kell on 10.10."}},
	} {
		if got := forkStream(c.chunks...); !reflect.DeepEqual(got, c.want) {
			t.Errorf("forkStream(%q) = %q, want %q", c.chunks, got, c.want)
		}
	}
}
