package main

import (
	"strings"
	"testing"
)

const journal = `Bot 0090927c Transcribed text (et-EE): mis on oecd?
Making LLM request for device 0090927c...
myvector llm: et-EE → ollama gemma4:e4b-it-qat @ http://localhost:11434/v1
LLM stream finished
See on majandusliit.
Kas tahad veel teada?
EOF
KG Interrupter has been stopped
myvector: follow-up 1/10 YES
Bot 0090927c Transcribed text (et-EE): ta kuulab ikka ju.
`

func TestParse(t *testing.T) {
	got := parse(strings.NewReader(journal))
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2", len(got))
	}
	if got[0].reply != "" || got[0].text != "mis on oecd?" {
		t.Errorf("item 0 = %+v", got[0])
	}
	if got[1].reply != "See on majandusliit. Kas tahad veel teada?" || got[1].text != "ta kuulab ikka ju." {
		t.Errorf("item 1 = %+v", got[1])
	}
}

func TestLabel(t *testing.T) {
	items := parse(strings.NewReader(journal))
	if err := label(items, strings.NewReader("speak\tmis on\nskip\tta kuulab\n")); err != nil {
		t.Fatal(err)
	}
	if items[0].want != "speak" || items[1].want != "skip" {
		t.Errorf("wants = %q %q", items[0].want, items[1].want)
	}
	if err := label(parse(strings.NewReader(journal)), strings.NewReader("skip\tnobody said this\n")); err == nil {
		t.Error("an unmatched label must fail")
	}
}

func TestPass(t *testing.T) {
	for _, c := range []struct {
		skipOK, nSkip, speakOK, nSpeak int
		want                           bool
	}{
		{12, 16, 6, 6, true},  // 75 %
		{11, 16, 6, 6, false}, // 68.75 %
		{16, 16, 5, 6, false}, // one genuine ask skipped
		{0, 0, 6, 6, false},   // nothing scored
	} {
		if got := pass(c.skipOK, c.nSkip, c.speakOK, c.nSpeak); got != c.want {
			t.Errorf("pass(%v) = %v", c, got)
		}
	}
}
