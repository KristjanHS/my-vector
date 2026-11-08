package memory

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// ExtractReplyCap is the extraction reply's max_tokens.
const ExtractReplyCap = 1024

// ErrParse is an extraction reply that is not the JSON array; the session is retried (ExtractRetries).
var ErrParse = errors.New("memory extract: reply is not a JSON array")

// ExtractPrompt is the extraction system message (M11).
var ExtractPrompt = `You read what a user said to their home robot and list durable facts about the user and their life.
Reply with only a JSON array, no prose: [{"category": "...", "fact": "...", "evidence": "...", "event_time": "..."}]
- category: one of ` + strings.Join(Categories, ", ") + `.
- fact: one short English sentence about the user, at most ` + fmt.Sprint(FactCap) + ` characters.
- evidence: the exact words from one user line that state the fact, copied verbatim in the original language, at most ` + fmt.Sprint(EvidenceCap) + ` characters.
- event_time: the date (YYYY-MM-DD) the fact is about when the user names one, relative days resolved from the line's date; else "".
Only facts the user states about themselves, their family, pets, home, work, health, likes, dislikes, routine or plans.
Skip questions, commands to the robot, small talk and anything the user only asks about. No facts → [].`

// ExtractUser is the extraction user message: s's user turns, one per line with their date.
func ExtractUser(s Session) string {
	var b strings.Builder
	for _, e := range s.Turns {
		if strings.TrimSpace(e.User) != "" {
			fmt.Fprintf(&b, "[%s] %s\n", e.At.Format("2006-01-02 Mon"), e.User)
		}
	}
	return b.String()
}

type candidate struct {
	Category  string `json:"category"`
	Fact      string `json:"fact"`
	Evidence  string `json:"evidence"`
	EventTime string `json:"event_time"`
}

// Extract asks t for s's facts; survivors of the category, cap and evidence checks come back Unconfirmed.
// A non-array reply wraps ErrParse; any other error is the model call's.
func Extract(ctx context.Context, t llm.Target, s Session, now time.Time) ([]Fact, error) {
	user := ExtractUser(s)
	if user == "" {
		return nil, nil
	}
	reply, err := Ask(ctx, t, ExtractPrompt, user, ExtractReplyCap)
	if err != nil {
		return nil, err
	}
	return ParseExtract(reply, s, now)
}

// ParseExtract is Extract's check of one model reply for s.
func ParseExtract(reply string, s Session, now time.Time) ([]Fact, error) {
	cands, err := parseCandidates(reply)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(s.ID))
	var out []Fact
	for i, c := range cands {
		c.Category = strings.ToLower(strings.TrimSpace(c.Category))
		c.Fact, c.Evidence = strings.TrimSpace(c.Fact), strings.TrimSpace(c.Evidence)
		if why := rejectCandidate(c, s); why != "" {
			logf("myvector: memory extract "+s.ID+" dropped ("+why+"):", c.Fact, "|", c.Evidence)
			continue
		}
		out = append(out, Fact{ID: fmt.Sprintf("%s-%d", hex.EncodeToString(sum[:4]), i), ESN: s.ESN, Category: c.Category,
			Text: c.Fact, Evidence: c.Evidence, EventAt: strings.TrimSpace(c.EventTime), LearnedAt: now, Session: s.ID, Status: Unconfirmed})
	}
	return out, nil
}

func parseCandidates(reply string) ([]candidate, error) {
	i, j := strings.Index(reply, "["), strings.LastIndex(reply, "]")
	if i < 0 || j < i {
		return nil, fmt.Errorf("%w: %q", ErrParse, reply)
	}
	var cs []candidate
	if err := json.Unmarshal([]byte(reply[i:j+1]), &cs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParse, err)
	}
	return cs, nil
}

func rejectCandidate(c candidate, s Session) string {
	switch {
	case !slices.Contains(Categories, c.Category):
		return "category " + c.Category
	case c.Fact == "" || utf8.RuneCountInString(c.Fact) > FactCap:
		return "fact length"
	case c.Evidence == "" || utf8.RuneCountInString(c.Evidence) > EvidenceCap:
		return "evidence length"
	}
	ev := fold(c.Evidence)
	for _, e := range s.Turns {
		if strings.Contains(fold(e.User), ev) {
			return ""
		}
	}
	return "evidence not in a user turn"
}

// fold case-folds s and collapses its whitespace (the evidence check, M11).
func fold(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// Admit records Extract's result for s: facts land (minus a Removed fact's evidence, M23) and s is done;
// ErrParse counts a retry, marking s done after ExtractRetries; any other error leaves s for the next pass.
func (st *Store) Admit(s Session, facts []Fact, err error) {
	switch {
	case errors.Is(err, ErrParse):
		if st.Retries[s.ID]++; st.Retries[s.ID] <= ExtractRetries {
			return
		}
		logf("myvector: memory extract "+s.ID+" given up after", st.Retries[s.ID], "unparsable replies")
	case err != nil:
		return
	}
	removed := map[string]bool{}
	for _, f := range st.Facts {
		if f.ESN == s.ESN && f.Status == Removed {
			removed[fold(f.Evidence)] = true
		}
	}
	for _, f := range facts {
		if removed[fold(f.Evidence)] {
			logf("myvector: memory extract "+s.ID+" dropped (removed before):", f.Text)
			continue
		}
		st.Facts = append(st.Facts, f)
	}
	delete(st.Retries, s.ID)
	st.Extracted = append(st.Extracted, s.ID)
}
