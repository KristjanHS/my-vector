package memory

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"sort"
	"time"
)

// API is the Memory card's endpoint on /myvector/prompt (M24).
const API = "/myvector/api/memory"

// RunTimeout bounds a "Run pass now" request.
const RunTimeout = 10 * time.Minute

// Pass is one full pass (memory target + robot zones), wired by ttr's init; a var so tests fake it.
var Pass = func(ctx context.Context) error { return errors.New("memory pass not wired") }

type robotView struct {
	ESN     string  `json:"esn"`
	Profile string  `json:"profile"`
	Facts   []Fact  `json:"facts"`
	Diary   []Entry `json:"diary"`
}

type apiReply struct {
	Model     string      `json:"model"`
	LastRun   time.Time   `json:"last_run"`
	LastGuard string      `json:"last_guard"`
	Robots    []robotView `json:"robots"`
}

// view is s per ESN, minus everything Removed.
func view(s *Store) apiReply {
	by := map[string]*robotView{}
	get := func(esn string) *robotView {
		if by[esn] == nil {
			by[esn] = &robotView{ESN: esn, Facts: []Fact{}, Diary: []Entry{}}
		}
		return by[esn]
	}
	for esn, p := range s.Profiles {
		get(esn).Profile = p.Text
	}
	for _, f := range s.Facts {
		if f.Status != Removed {
			get(f.ESN).Facts = append(get(f.ESN).Facts, f)
		}
	}
	for _, e := range s.Diary {
		if e.Status != Removed {
			get(e.ESN).Diary = append(get(e.ESN).Diary, e)
		}
	}
	out := apiReply{Model: Model(), LastRun: s.LastRun, LastGuard: s.LastGuard, Robots: []robotView{}}
	for _, v := range by {
		out.Robots = append(out.Robots, *v)
	}
	sort.Slice(out.Robots, func(i, j int) bool { return out.Robots[i].ESN < out.Robots[j].ESN })
	return out
}

// Remove flags fact or diary entry id Removed (M23); removing an active fact drops its ESN's profile, rebuilt next pass.
// false = no such id.
func (st *Store) Remove(id string, now time.Time) bool {
	for i := range st.Facts {
		if f := &st.Facts[i]; f.ID == id {
			if f.Status == Active {
				delete(st.Profiles, f.ESN)
			}
			if f.Status != Removed {
				f.Status, f.RemovedAt = Removed, &now
			}
			return true
		}
	}
	for i := range st.Diary {
		if e := &st.Diary[i]; e.ID == id {
			if e.Status != Removed {
				e.Status, e.RemovedAt = Removed, &now
			}
			return true
		}
	}
	return false
}

var errNoID = errors.New("no such fact or diary entry")

// APIHandler serves API: GET lists; POST application/json {action: remove, id} or {action: run} (bypasses the idle wait,
// not the single-flight: busy → 409), then lists.
func APIHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		var q struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if err := d.Decode(&q); err != nil {
			http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		switch q.Action {
		case "remove":
			err := Update(func(st *Store) error {
				if !st.Remove(q.ID, Now()) {
					return errNoID
				}
				return nil
			})
			if errors.Is(err, errNoID) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		case "run":
			ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
			err := Pass(ctx)
			cancel()
			if errors.Is(err, ErrBusy) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			} else if err != nil {
				http.Error(w, "pass failed: "+err.Error(), http.StatusBadGateway)
				return
			}
		default:
			http.Error(w, `action: "remove" or "run"`, http.StatusBadRequest)
			return
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		return
	}
	s, err := Read()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(view(s))
}
