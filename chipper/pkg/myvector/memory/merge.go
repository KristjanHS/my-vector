package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// MergeReplyCap is the merge reply's max_tokens.
const MergeReplyCap = 512

// MergeRetries is how many passes a candidate named in no op stays Unconfirmed before it turns Rejected.
const MergeRetries = 3

// Merge op kinds.
const (
	Confirm   = "confirm"
	Supersede = "supersede"
	Reject    = "reject"
)

// MergePrompt is the merge system message (M12).
var MergePrompt = `You keep a home robot's memory of its user tidy. You get one category's known facts (a1, a2, …) and new candidate facts (c1, c2, …).
Reply with only action lines, one per candidate, no prose:
confirm cN — a new fact worth keeping that no known fact or earlier candidate already says.
supersede aN cN — cN replaces known fact aN: the fact changed, or cN corrects it.
reject cN — cN repeats a known fact or another candidate, is not about the user, or is not durable.
Known facts stay unless a supersede line names them.`

// Batch is one ESN category's merge input: its Active facts and Unconfirmed candidates.
type Batch struct {
	ESN, Category string
	Active, Cands []Fact
}

// Batches are esn's categories that have candidates, in Categories order — an untouched category is never sent (M12).
func (st *Store) Batches(esn string) []Batch {
	var out []Batch
	for _, c := range Categories {
		if b := st.Batch(esn, c); len(b.Cands) > 0 {
			out = append(out, b)
		}
	}
	return out
}

// Batch is esn's category c merge input.
func (st *Store) Batch(esn, c string) Batch {
	b := Batch{ESN: esn, Category: c}
	for _, f := range st.Facts {
		switch {
		case f.ESN != esn || f.Category != c:
		case f.Status == Active:
			b.Active = append(b.Active, f)
		case f.Status == Unconfirmed:
			b.Cands = append(b.Cands, f)
		}
	}
	return b
}

// MergeUser is the merge user message: b's facts under their aliases.
func MergeUser(b Batch) string {
	var s strings.Builder
	fmt.Fprintf(&s, "Category: %s\nKnown facts:\n", b.Category)
	if len(b.Active) == 0 {
		s.WriteString("(none)\n")
	}
	for i, f := range b.Active {
		fmt.Fprintf(&s, "a%d: %s\n", i+1, f.Text)
	}
	s.WriteString("Candidates:\n")
	for i, f := range b.Cands {
		fmt.Fprintf(&s, "c%d: %s (said: %q)\n", i+1, f.Text, f.Evidence)
	}
	return s.String()
}

// Op is one parsed merge action on fact IDs: ID is the candidate, Old the active fact a Supersede replaces.
type Op struct{ Kind, ID, Old string }

// Merge asks t for b's ops; an error is the model call's (the batch waits for the next pass, uncounted).
func Merge(ctx context.Context, t llm.Target, b Batch) ([]Op, error) {
	reply, err := Ask(ctx, t, MergePrompt, MergeUser(b), MergeReplyCap)
	if err != nil {
		return nil, err
	}
	return ParseMerge(reply, b), nil
}

// ParseMerge maps reply's action lines to ops on b's fact IDs; any other line is dropped, a malformed action logged.
func ParseMerge(reply string, b Batch) []Op {
	var ops []Op
	for _, line := range strings.Split(reply, "\n") {
		w := strings.FieldsFunc(strings.ToLower(line), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
		for len(w) > 0 && strings.Trim(w[0], "0123456789") == "" { // "1. confirm c1"
			w = w[1:]
		}
		if len(w) == 0 || (w[0] != Confirm && w[0] != Reject && w[0] != Supersede) {
			continue
		}
		var op Op
		switch {
		case w[0] == Supersede && len(w) >= 3:
			op = Op{Kind: Supersede, Old: alias(w[1], "a", b.Active), ID: alias(w[2], "c", b.Cands)}
		case w[0] != Supersede && len(w) >= 2:
			op = Op{Kind: w[0], ID: alias(w[1], "c", b.Cands)}
		}
		if op.ID == "" || op.Kind == Supersede && op.Old == "" {
			logf("myvector: memory merge "+b.ESN+" "+b.Category+" refused:", strings.TrimSpace(line))
			continue
		}
		ops = append(ops, op)
	}
	return ops
}

// alias is fs[n-1]'s ID for a = prefix+n, else "" — so "reject a1" (an active fact with no replacement) never parses.
func alias(a, prefix string, fs []Fact) string {
	n, err := strconv.Atoi(strings.TrimPrefix(a, prefix))
	if !strings.HasPrefix(a, prefix) || err != nil || n < 1 || n > len(fs) {
		return ""
	}
	return fs[n-1].ID
}

// ApplyMerge applies ops to b's facts as they stand now (a candidate's first confirm/reject wins, a supersede may follow its
// confirm; a fact a user removed meanwhile is left alone) and counts a try on every candidate no op applied to. changed = the active set moved, so the profile is owed.
func (st *Store) ApplyMerge(b Batch, ops []Op) (changed bool) {
	idx := map[string]*Fact{}
	for i := range st.Facts {
		if f := &st.Facts[i]; f.ESN == b.ESN && f.Category == b.Category {
			idx[f.ID] = f
		}
	}
	inBatch := map[string]bool{}
	for _, f := range b.Cands {
		inBatch[f.ID] = true
	}
	for _, op := range ops {
		c := idx[op.ID]
		if c == nil || !inBatch[op.ID] {
			continue
		}
		switch {
		case op.Kind == Confirm && c.Status == Unconfirmed:
			c.Status, changed = Active, true
		case op.Kind == Reject && c.Status == Unconfirmed:
			c.Status = Rejected
		case op.Kind == Supersede && (c.Status == Unconfirmed || c.Status == Active):
			old := idx[op.Old]
			if old == nil || old.Status != Active {
				continue
			}
			old.Status, old.SupersededBy = Superseded, c.ID
			c.Status, changed = Active, true
		}
	}
	for _, f := range b.Cands {
		if c := idx[f.ID]; c != nil && c.Status == Unconfirmed { // no op applied to it
			tried(c, b.ESN, "passes unnamed")
		}
	}
	return changed
}

// tried counts one merge try on candidate c, Rejected past MergeRetries (M12, M14).
func tried(c *Fact, esn, why string) {
	if c.Tries++; c.Tries > MergeRetries {
		c.Status = Rejected
		logf("myvector: memory merge "+esn+" "+c.Category+" rejected after", c.Tries, why+":", c.Text)
	}
}
