package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// recordModel is an Ollama target that answers each request with reply(user message) and records the user messages.
func recordModel(t *testing.T, reply func(user string) string) (llm.Target, *[]string) {
	t.Helper()
	var users []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct{ Role, Content string }
		}
		json.NewDecoder(r.Body).Decode(&req)
		u := req.Messages[len(req.Messages)-1].Content
		users = append(users, u)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": reply(u)}}}})
	}))
	t.Cleanup(s.Close)
	return llm.Target{Provider: llm.Ollama, BaseURL: s.URL + "/v1", Key: "ollama", Model: llm.DefaultETModel}, &users
}

func mfact(id, cat, status, text string) Fact {
	return Fact{ID: id, ESN: esn, Category: cat, Text: text, Evidence: text, Status: status}
}

func status(st *Store) map[string]string {
	m := map[string]string{}
	for _, f := range st.Facts {
		m[f.ID] = f.Status
	}
	return m
}

func TestMerge(t *testing.T) {
	logs := useModel(t, `{}`)
	st := (&Store{Facts: []Fact{
		mfact("p1", "pets", Active, "The user has a cat named Miisu."),
		mfact("w1", "work", Active, "The user works at a library."),
		mfact("pc1", "pets", Unconfirmed, "The user has a dog named Muki."),
		mfact("pc2", "pets", Unconfirmed, "The user's cat Miisu died; they have no cat now."),
		mfact("pc3", "pets", Unconfirmed, "The user likes dogs."),
		mfact("pc4", "pets", Unconfirmed, "The user's dog is called Muki."),
		mfact("x1", "pets", Unconfirmed, "Another robot's fact."),
	}}).init()
	st.Facts[len(st.Facts)-1].ESN = "other"

	bs := st.Batches(esn)
	if len(bs) != 1 || bs[0].Category != "pets" || len(bs[0].Active) != 1 || len(bs[0].Cands) != 4 {
		t.Fatalf("Batches = %+v; want only pets, this ESN", bs)
	}
	tg, users := recordModel(t, func(string) string {
		return "Here you go:\n1. confirm c1\n- supersede a1 c2\nsupersede a1 c4\nreject a1\nconfirm c9\nconfirm c3 — likes\nreject c3\n"
	})
	ops, err := Merge(context.Background(), tg, bs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(*users) != 1 || strings.Contains((*users)[0], "library") || !strings.Contains((*users)[0], "a1: The user has a cat named Miisu.") {
		t.Fatalf("an untouched category must never be sent; sent %q", *users)
	}
	if !st.ApplyMerge(bs[0], ops) {
		t.Fatal("confirm + supersede must report the active set changed")
	}
	want := map[string]string{"p1": Superseded, "w1": Active, "pc1": Active, "pc2": Active, "pc3": Active, "pc4": Unconfirmed, "x1": Unconfirmed}
	if got := status(st); !mapsEqual(got, want) {
		t.Fatalf("status = %v, want %v", got, want)
	}
	if st.Facts[0].SupersededBy != "pc2" {
		t.Fatalf("superseded_by = %q, want pc2", st.Facts[0].SupersededBy)
	}
	if st.Facts[5].Tries != 1 {
		t.Fatalf("unnamed candidate tries = %d, want 1", st.Facts[5].Tries)
	}
	refused := 0
	for _, l := range *logs {
		if strings.Contains(l, "refused") {
			refused++
		}
	}
	if refused != 2 {
		t.Fatalf("want 'reject a1' and 'confirm c9' refused, logs %q", *logs)
	}
}

func TestMergeUnnamedRejected(t *testing.T) {
	useModel(t, `{}`)
	st := (&Store{Facts: []Fact{mfact("c", "pets", Unconfirmed, "The user has a dog.")}}).init()
	for pass := 1; pass <= MergeRetries+1; pass++ {
		bs := st.Batches(esn)
		if len(bs) != 1 || len(bs[0].Active) != 0 {
			t.Fatalf("pass %d: a category with no active facts must still be sent: %+v", pass, bs)
		}
		if st.ApplyMerge(bs[0], ParseMerge("Nothing to do.", bs[0])) {
			t.Fatalf("pass %d: nothing named, nothing changed", pass)
		}
		if got, want := st.Facts[0].Status, Unconfirmed; pass > MergeRetries {
			want = Rejected
			if got != want {
				t.Fatalf("pass %d: status %s, want %s", pass, got, want)
			}
		} else if got != want || st.Facts[0].Tries != pass {
			t.Fatalf("pass %d: status %s tries %d", pass, got, st.Facts[0].Tries)
		}
	}
	if len(st.Batches(esn)) != 0 {
		t.Fatal("a rejected candidate must not be sent again")
	}
}

func TestMergeReject(t *testing.T) {
	useModel(t, `{}`)
	st := (&Store{Facts: []Fact{mfact("a", "pets", Active, "x"), mfact("c", "pets", Unconfirmed, "y")}}).init()
	b := st.Batches(esn)[0]
	if st.ApplyMerge(b, ParseMerge("reject c1\nconfirm c1", b)) || st.Facts[1].Status != Rejected || st.Facts[0].Status != Active {
		t.Fatalf("reject wins as the first op, active untouched: %v", status(st))
	}
}

func TestProfile(t *testing.T) {
	logs := useModel(t, `{}`)
	st := (&Store{Facts: []Fact{mfact("a", "pets", Active, "The user has a dog named Muki."), mfact("b", "pets", Superseded, "old")}}).init()
	st.Profiles[esn] = Profile{Text: "old profile"}
	fs := st.ActiveFacts(esn)
	if len(fs) != 1 {
		t.Fatalf("ActiveFacts = %+v", fs)
	}
	long := strings.Repeat("word ", 3*ProfileCap)
	tg, users := recordModel(t, func(string) string { return long })
	p, err := BuildProfile(context.Background(), tg, fs)
	if st.SetProfile(esn, p, err, t0) || st.Profiles[esn].Text != "old profile" {
		t.Fatalf("over-cap profile must keep the old one: %q", st.Profiles[esn].Text)
	}
	if len(*users) != 1 || strings.Contains((*users)[0], "old") {
		t.Fatalf("profile is built from active facts only: %q", *users)
	}
	if st.SetProfile(esn, "", errors.New("down"), t0) || len(*logs) != 2 {
		t.Fatalf("a model error keeps the old profile, logged: %q", *logs)
	}
	if !st.SetProfile(esn, "The user has a dog named Muki.", nil, t0) || st.Profiles[esn].Text != "The user has a dog named Muki." {
		t.Fatal("a fitting profile is stored")
	}
	if p, err := BuildProfile(context.Background(), tg, nil); p != "" || err != nil || len(*users) != 1 {
		t.Fatal("no active facts → empty profile, no model call")
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
