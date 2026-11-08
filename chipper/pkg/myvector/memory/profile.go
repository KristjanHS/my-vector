package memory

import (
	"context"
	"errors"
	"fmt"
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

// SetProfile stores text as esn's profile; err or text over ProfileCap keeps the old one (logged). Reports whether it changed.
func (st *Store) SetProfile(esn, text string, err error, now time.Time) bool {
	switch n := prompt.Estimate(text); {
	case err != nil:
		logf("myvector: memory profile "+esn+" kept:", err)
		return false
	case n > ProfileCap:
		logf("myvector: memory profile "+esn+" kept: reply", n, "tokens over", ProfileCap)
		return false
	}
	st.Profiles[esn] = Profile{Text: text, At: now}
	return true
}
