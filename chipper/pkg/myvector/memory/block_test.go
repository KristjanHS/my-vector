package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

const esn = "00e20100"

var t0 = time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)

func fact(id, text, evidence string, at time.Time) Fact {
	return Fact{ID: id, ESN: esn, Category: "pets", Text: text, Evidence: evidence, LearnedAt: at, Status: Active}
}

func TestBlockBudget(t *testing.T) {
	s := (&Store{}).init()
	s.Profiles[esn] = Profile{Text: strings.Repeat("p", 3*ProfileCap)}
	for i := 0; i < FactsClaude; i++ {
		s.Facts = append(s.Facts, fact(fmt.Sprint(i), strings.Repeat("f", FactCap), strings.Repeat("e", EvidenceCap), t0))
	}
	for i := 0; i < DiaryClaude; i++ {
		s.Diary = append(s.Diary, Entry{ID: fmt.Sprint(i), ESN: esn, Day: "2026-11-07", Text: strings.Repeat("d", DiaryCap)})
	}
	for _, brain := range []string{prompt.Ollama, prompt.Claude} {
		facts, diary := Block(s, esn, "anything", brain)
		if n := prompt.Estimate(facts) + prompt.Estimate(diary); n > prompt.MemoryBudget(brain) {
			t.Errorf("%s: %d tokens > %d", brain, n, prompt.MemoryBudget(brain))
		}
		if !strings.Contains(facts, s.Profiles[esn].Text) {
			t.Errorf("%s: profile dropped", brain)
		}
	}
	// Claude fits everything: 30 facts and 7 entries all render.
	facts, diary := Block(s, esn, "anything", prompt.Claude)
	if n := strings.Count(facts, "\n- "); n != FactsClaude {
		t.Errorf("claude facts = %d, want %d", n, FactsClaude)
	}
	if n := strings.Count(diary, "\n- "); n != DiaryClaude {
		t.Errorf("claude diary = %d, want %d", n, DiaryClaude)
	}
}

func TestRetrieve(t *testing.T) {
	s := (&Store{}).init()
	s.Facts = []Fact{
		fact("new", "The user works as a baker.", "ma olen pagar", t0.Add(time.Hour)),
		fact("dog", "The user has a dog named Muki.", "mu koer Muki", t0),
		fact("cat", "The user had a cat.", "mu kass", t0.Add(2*time.Hour)),
	}
	s.Facts[2].Status = Superseded
	got := Retrieve(s, esn, "kas mu koer Muki on terve?")
	if len(got) != 2 || got[0].ID != "dog" || got[1].ID != "new" {
		t.Fatalf("order = %v", got)
	}
	if got := Retrieve(s, esn, "tere"); got[0].ID != "new" {
		t.Errorf("tie → newest first, got %s", got[0].ID)
	}
}

func TestBlockSkipsRemovedAndSuperseded(t *testing.T) {
	s := (&Store{}).init()
	s.Facts = []Fact{fact("a", "Kept fact.", "x", t0), fact("b", "Removed fact.", "x", t0), fact("c", "Old fact.", "x", t0)}
	s.Facts[1].Status, s.Facts[2].Status = Removed, Superseded
	s.Diary = []Entry{{ESN: esn, Day: "2026-11-06", Text: "Kept day."}, {ESN: esn, Day: "2026-11-07", Text: "Removed day.", Status: Removed}}
	facts, diary := Block(s, esn, "", prompt.Claude)
	if !strings.Contains(facts, "Kept fact.") || strings.Contains(facts, "Removed fact.") || strings.Contains(facts, "Old fact.") {
		t.Errorf("facts = %q", facts)
	}
	if !strings.Contains(diary, "Kept day.") || strings.Contains(diary, "Removed day.") {
		t.Errorf("diary = %q", diary)
	}
	if f, d := Block(s, "other", "", prompt.Claude); f != "" || d != "" {
		t.Errorf("other ESN: %q %q", f, d)
	}
}

func TestStoreCorruptResets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorMemory.json")
	old := Path
	Path = func() string { return path }
	t.Cleanup(func() { Path = old })
	os.WriteFile(path, []byte("{"), 0600)
	if err := Update(func(s *Store) error { s.LastGuard = "ok"; return nil }); err != nil {
		t.Fatal(err)
	}
	s, err := Read()
	if err != nil || s.LastGuard != "ok" || s.Profiles == nil {
		t.Fatalf("store = %+v, %v", s, err)
	}
}
