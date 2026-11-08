package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/prompt"
)

// ProfileReplyCap is the profile reply's max_tokens: room past ProfileCap, so an over-long reply is refused, not cut.
const ProfileReplyCap = 2 * ProfileCap

// ProfilePrompt is the profile system message (M13).
var ProfilePrompt = `You write a short English profile of a home robot's user from the facts below, for the robot to read before it answers.
Plain third-person sentences ("The user …"), only what the facts say, most useful first, at most ` + fmt.Sprint(ProfileCap/3) + ` words. Reply with only the profile.`

// ActiveFacts are esn's Active facts in store order.
func (st *Store) ActiveFacts(esn string) []Fact {
	var out []Fact
	for _, f := range st.Facts {
		if f.ESN == esn && f.Status == Active {
			out = append(out, f)
		}
	}
	return out
}

// ProfileUser is the profile user message: one fact per line with its category.
func ProfileUser(fs []Fact) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "- [%s] %s\n", f.Category, f.Text)
	}
	return b.String()
}

// BuildProfile asks t for a profile of fs; no facts → "" with no call, an empty reply is an error.
func BuildProfile(ctx context.Context, t llm.Target, fs []Fact) (string, error) {
	if len(fs) == 0 {
		return "", nil
	}
	reply, err := Ask(ctx, t, ProfilePrompt, ProfileUser(fs), ProfileReplyCap)
	if err == nil && reply == "" {
		err = errors.New("memory profile: empty reply")
	}
	return reply, err
}

// SetProfile is SetProfileOf with esn's active facts in st as the inputs.
func (st *Store) SetProfile(esn, text string, err error, now time.Time) bool {
	return st.SetProfileOf(esn, text, st.ActiveFacts(esn), err, now)
}

// SetProfileOf stores text, built from facts from, as esn's profile; err or text over ProfileCap keeps the old one,
// an input now Removed drops text (M26), both logged. Reports whether it changed.
func (st *Store) SetProfileOf(esn, text string, from []Fact, err error, now time.Time) bool {
	ids := make([]string, 0, len(from))
	for _, f := range from {
		ids = append(ids, f.ID)
	}
	p := Profile{Text: text, At: now, From: ids}
	switch n := prompt.Estimate(text); {
	case err != nil:
		logf("myvector: memory profile "+esn+" kept:", err)
		return false
	case n > ProfileCap:
		logf("myvector: memory profile "+esn+" kept: reply", n, "tokens over", ProfileCap)
		return false
	case st.stale(esn, p):
		logf("myvector: memory profile " + esn + " dropped: an input fact was removed meanwhile")
		return false
	}
	st.Profiles[esn] = p
	return true
}

// fed reports whether fact id is one of p's inputs; a profile with no From (pre-M26) counts as fed by every fact.
func (p Profile) fed(id string) bool { return p.From == nil || slices.Contains(p.From, id) }

// stale reports whether one of p's input facts is Removed in st (M26).
func (st *Store) stale(esn string, p Profile) bool {
	for _, f := range st.Facts {
		if f.ESN == esn && f.Status == Removed && p.fed(f.ID) {
			return true
		}
	}
	return false
}

// DropRemoved marks st's esn facts Removed where the stored copy has them Removed, and drops a profile they fed, so a
// Remove made mid-pass reaches no later model call (M26).
func (st *Store) DropRemoved(esn string) error {
	cur, err := Read()
	if err != nil {
		return err
	}
	removed := map[string]*time.Time{}
	for _, f := range cur.Facts {
		if f.ESN == esn && f.Status == Removed {
			removed[f.ID] = f.RemovedAt
		}
	}
	for i := range st.Facts {
		if f := &st.Facts[i]; f.ESN == esn && f.Status != Removed {
			if at, ok := removed[f.ID]; ok {
				f.Status, f.RemovedAt = Removed, at
			}
		}
	}
	if p, ok := st.Profiles[esn]; ok && st.stale(esn, p) {
		delete(st.Profiles, esn)
	}
	return nil
}
