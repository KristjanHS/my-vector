package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

func serveAPI(method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, API, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	APIHandler(w, r)
	return w
}

func TestMemoryAPI(t *testing.T) {
	useEpisodes(t, `{"memory_model":"off"}`)
	t0 := time.Date(2026, 11, 7, 9, 0, 0, 0, time.UTC)
	seed := func(st *Store) error {
		st.Facts = []Fact{fact("f1", "The user's dog is called Muki.", "mu koer Muki", t0), fact("f2", "The user lives in Tartu.", "elan Tartus", t0)}
		st.Diary = []Entry{{ID: esn + "@2026-11-06", ESN: esn, Day: "2026-11-06", Text: "We talked about Muki."}}
		st.Profiles[esn] = Profile{Text: "Lives in Tartu, has a dog Muki."}
		return nil
	}
	if err := Update(seed); err != nil {
		t.Fatal(err)
	}
	list := func(w *httptest.ResponseRecorder) apiReply {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		var got apiReply
		json.Unmarshal(w.Body.Bytes(), &got)
		return got
	}

	got := list(serveAPI(http.MethodGet, ""))
	if len(got.Robots) != 1 || len(got.Robots[0].Facts) != 2 || got.Robots[0].Facts[0].Evidence != "mu koer Muki" ||
		len(got.Robots[0].Diary) != 1 || got.Robots[0].Profile == "" || got.Model != "off" {
		t.Fatalf("GET %+v", got)
	}

	got = list(serveAPI(http.MethodPost, `{"action":"remove","id":"f1"}`))
	if fs := got.Robots[0].Facts; len(fs) != 1 || fs[0].ID != "f2" || got.Robots[0].Profile != "" {
		t.Fatalf("after remove: facts %+v, profile %q (an active fact's removal drops the profile)", fs, got.Robots[0].Profile)
	}
	st, _ := Read()
	if f := st.Facts[0]; f.Status != Removed || f.RemovedAt == nil {
		t.Fatalf("removed fact must stay in the file flagged: %+v", f)
	}
	if facts, _ := Block(st, esn, "kas mu koer Muki on terve?", prompt.Claude); strings.Contains(facts, "Muki") {
		t.Fatalf("Block still renders the removed fact: %q", facts)
	}
	got = list(serveAPI(http.MethodPost, `{"action":"remove","id":"`+esn+`@2026-11-06"}`))
	if len(got.Robots[0].Diary) != 0 {
		t.Fatalf("diary entry not removed: %+v", got.Robots[0].Diary)
	}
	if w := serveAPI(http.MethodPost, `{"action":"remove","id":"nope"}`); w.Code != 404 {
		t.Errorf("remove unknown id: %d, want 404", w.Code)
	}

	runs, oldPass := 0, Pass
	Pass = func(context.Context) error { runs++; return nil }
	t.Cleanup(func() { Pass = oldPass })
	list(serveAPI(http.MethodPost, `{"action":"run"}`))
	if runs != 1 {
		t.Fatalf("run: Pass called %d times, want 1", runs)
	}
	Pass = func(context.Context) error { return ErrBusy }
	if w := serveAPI(http.MethodPost, `{"action":"run"}`); w.Code != 409 {
		t.Errorf("run while busy: %d, want 409", w.Code)
	}

	for _, bad := range []string{`{"action":"forget"}`, `{}`, `{"action":"run","x":1}`, `nope`} {
		if w := serveAPI(http.MethodPost, bad); w.Code != 400 {
			t.Errorf("POST %s: %d, want 400", bad, w.Code)
		}
	}
}

func TestMemoryCardOnPromptPage(t *testing.T) {
	w := httptest.NewRecorder()
	prompt.PageHandler(w, httptest.NewRequest(http.MethodGet, prompt.PageURL, nil))
	for _, want := range []string{`"` + API + `"`, `<h2>Memory</h2>`, `action: "remove", id`, `{ action: "run" }`, `f.evidence`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("prompt page lacks %s", want)
		}
	}
}
