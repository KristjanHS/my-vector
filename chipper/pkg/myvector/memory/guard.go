package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// GuardReplyCap is a guard answer's max_tokens.
const GuardReplyCap = 64

// Unknown is the guard answer for a question the memory cannot answer.
const Unknown = "UNKNOWN"

// Questions are the guard's fixed probes, one per Categories entry, keyed by category (M14).
var Questions = map[string]string{
	"identity": "What is the user's name?",
	"family":   "Who is in the user's family?",
	"pets":     "What pets does the user have?",
	"home":     "Where does the user live?",
	"work":     "What does the user do for work?",
	"health":   "What health conditions does the user have?",
	"likes":    "What does the user like?",
	"dislikes": "What does the user dislike?",
	"routine":  "What is the user's daily routine?",
	"plans":    "What plans does the user have?",
}

// GuardPrompt is the guard system message.
var GuardPrompt = `Answer the question about a home robot's user from the profile and facts given, nothing else.
Reply with a short answer, or exactly ` + Unknown + ` when they do not answer it.`

// GuardUser is one guard question's user message: esn's profile, the category's active facts, the question.
func GuardUser(st *Store, esn, category string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Profile: %s\nFacts:\n", st.Profiles[esn].Text)
	for _, f := range st.ActiveFacts(esn) {
		if f.Category == category {
			fmt.Fprintf(&b, "- %s\n", f.Text)
		}
	}
	fmt.Fprintf(&b, "Question: %s\n", Questions[category])
	return b.String()
}

// Answers asks t every question against st; known[category] = the answer was not Unknown.
func Answers(ctx context.Context, t llm.Target, st *Store, esn string) (known map[string]bool, err error) {
	known = map[string]bool{}
	for _, c := range Categories {
		a, err := Ask(ctx, t, GuardPrompt, GuardUser(st, esn, c), GuardReplyCap)
		if err != nil {
			return nil, err
		}
		known[c] = !strings.Contains(strings.ToUpper(a), Unknown)
	}
	return known, nil
}

// Lost are the categories known before and not after, in Categories order.
func Lost(before, after map[string]bool) []string {
	var out []string
	for _, c := range Categories {
		if before[c] && !after[c] {
			out = append(out, c)
		}
	}
	return out
}

// Snapshot is esn's facts and profile before a merge, restored by Revert.
type Snapshot struct {
	ESN        string
	Facts      []byte // esn's facts as JSON, a deep copy
	Profile    Profile
	HadProfile bool
}

// Snapshot copies esn's facts and profile.
func (st *Store) Snapshot(esn string) Snapshot {
	var fs []Fact
	for _, f := range st.Facts {
		if f.ESN == esn {
			fs = append(fs, f)
		}
	}
	b, _ := json.Marshal(fs)
	p, ok := st.Profiles[esn]
	return Snapshot{ESN: esn, Facts: b, Profile: p, HadProfile: ok}
}

// Guard records the guard's verdict in LastGuard; lost ≠ nil also puts back snap's facts and profile (candidates are
// Unconfirmed again, retried next pass), keeping any Removed status a user set meanwhile.
func (st *Store) Guard(snap Snapshot, lost []string, now time.Time) {
	st.LastGuard = snap.ESN + " " + now.Format(time.RFC3339) + " ok"
	if len(lost) == 0 {
		return
	}
	st.LastGuard = snap.ESN + " " + now.Format(time.RFC3339) + " revert " + strings.Join(lost, ",")
	removed := map[string]*time.Time{}
	keep := st.Facts[:0:0]
	for _, f := range st.Facts {
		switch {
		case f.ESN != snap.ESN:
			keep = append(keep, f)
		case f.Status == Removed:
			removed[f.ID] = f.RemovedAt
		}
	}
	var old []Fact
	json.Unmarshal(snap.Facts, &old)
	for _, f := range old {
		if at, ok := removed[f.ID]; ok {
			f.Status, f.RemovedAt = Removed, at
		}
		keep = append(keep, f)
	}
	st.Facts = keep
	if snap.HadProfile {
		st.Profiles[snap.ESN] = snap.Profile
	} else {
		delete(st.Profiles, snap.ESN)
	}
	logf("myvector: memory guard "+snap.ESN+" reverted the merge, lost:", strings.Join(lost, ","))
}
