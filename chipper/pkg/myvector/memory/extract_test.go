package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

func turn(min float64, user string) Episode {
	return Episode{At: t0.Add(time.Duration(min * float64(time.Minute))), Kind: Chat, User: user, Lang: "et-EE"}
}

func TestSessions(t *testing.T) {
	eps := []Episode{turn(0, "a"), turn(9, "b"), turn(19, "c"), turn(30, "d")}
	got := Sessions("esn1", eps, t0.Add(35*time.Minute))
	if len(got) != 2 || len(got[0].Turns) != 2 || len(got[1].Turns) != 1 || got[1].Turns[0].User != "c" {
		t.Fatalf("9 min gap joins, 10 min splits, open last left out; got %+v", got)
	}
	if got[0].ID != "esn1@2026-11-07T09:00:00Z" || got[0].ESN != "esn1" {
		t.Fatalf("session id %q esn %q", got[0].ID, got[0].ESN)
	}
	if got := Sessions("esn1", eps, t0.Add(40*time.Minute)); len(got) != 3 {
		t.Fatalf("closed at 10 min after the last turn: %d sessions, want 3", len(got))
	}
	st := &Store{Extracted: []string{got[0].ID}}
	if p := st.Pending(got); len(p) != 1 || p[0].ID != got[1].ID {
		t.Fatalf("Pending = %+v, want only the unextracted session", p)
	}
}

func TestEpisodesSorted(t *testing.T) {
	useEpisodes(t, `{}`)
	for _, e := range []Episode{turn(5, "late"), turn(0, "early")} {
		if err := Capture("esn1", e, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	p := EpisodePath("esn1", "2026-11-06")
	os.WriteFile(p, []byte("{bad\n"+`{"at":"2026-11-06T10:00:00Z","user":"yesterday"}`+"\n"), 0600)
	eps, err := Episodes("esn1")
	if err != nil || len(eps) != 3 || eps[0].User != "yesterday" || eps[2].User != "late" {
		t.Fatalf("Episodes = %+v, %v", eps, err)
	}
}

// fakeModel is an Ollama target whose every reply is content.
func fakeModel(t *testing.T, content string) llm.Target {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}}})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return llm.Target{Provider: llm.Ollama, BaseURL: s.URL + "/v1", Key: "ollama", Model: llm.DefaultETModel}
}

func TestExtract(t *testing.T) {
	logs := useModel(t, `{}`)
	s := Session{ID: "esn1@x", ESN: "esn1", Turns: []Episode{turn(0, "Mu koer   Muki on kolmeaastane."), turn(1, "Ma töötan raamatukogus.")}}
	reply := "```json\n" + `[
		{"category": "Pets", "fact": "The user's dog Muki is three.", "evidence": "mu KOER muki on kolmeaastane", "event_time": ""},
		{"category": "work", "fact": "The user is a librarian.", "evidence": "ma olen raamatukoguhoidja", "event_time": ""},
		{"category": "hobbies", "fact": "The user works in a library.", "evidence": "Ma töötan raamatukogus", "event_time": ""}
	]` + "\n```"
	got, err := Extract(context.Background(), fakeModel(t, reply), s, t0)
	if err != nil || len(got) != 1 {
		t.Fatalf("Extract = %+v, %v; want only the verbatim-evidence fact", got, err)
	}
	if f := got[0]; f.Status != Unconfirmed || f.Category != "pets" || f.ESN != "esn1" || f.Session != "esn1@x" || !f.LearnedAt.Equal(t0) || f.ID == "" {
		t.Fatalf("fact %+v", f)
	}
	if len(*logs) != 2 {
		t.Fatalf("want the paraphrase and the unknown category logged, got %q", *logs)
	}

	st := &Store{Retries: map[string]int{}, Facts: []Fact{{ESN: "esn1", Status: Removed, Evidence: "Mu koer Muki on kolmeaastane"}}}
	st.Admit(s, got, nil)
	if len(st.Facts) != 1 || len(st.Extracted) != 1 {
		t.Fatalf("a removed fact's evidence must not come back: facts %+v extracted %v", st.Facts, st.Extracted)
	}
}

func TestExtractRetries(t *testing.T) {
	useModel(t, `{}`)
	s := Session{ID: "esn1@y", ESN: "esn1", Turns: []Episode{turn(0, "Mul on kass.")}}
	tg := fakeModel(t, "Sure! Here are the facts: none.")
	st := &Store{Retries: map[string]int{}}
	for i := 1; i <= ExtractRetries+1; i++ {
		facts, err := Extract(context.Background(), tg, s, t0)
		if !errors.Is(err, ErrParse) {
			t.Fatalf("try %d: err %v, want ErrParse", i, err)
		}
		st.Admit(s, facts, err)
		if done := len(st.Extracted) == 1; done != (i > ExtractRetries) {
			t.Fatalf("try %d: retries %v extracted %v", i, st.Retries, st.Extracted)
		}
	}
	if _, ok := st.Retries[s.ID]; ok {
		t.Fatal("retry count kept after the session was marked done")
	}
	st.Admit(Session{ID: "esn1@z"}, nil, errors.New("connection refused"))
	if len(st.Extracted) != 1 || st.Retries["esn1@z"] != 0 {
		t.Fatal("a model call error must leave the session for the next pass, uncounted")
	}
}
