package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// DiaryReplyCap is a diary reply's max_tokens.
const DiaryReplyCap = 256

// RetainEpisodes is how long an episode file is kept, by its date (M6).
const RetainEpisodes = 180 * 24 * time.Hour

// DiaryPrompt is the diary system message (M15).
var DiaryPrompt = `You are a home robot writing one short diary entry in English about your day with your user, from the conversation lines given.
Say what you talked about and anything notable about the user, in the first person ("Today Kalle asked me …"), at most ` + fmt.Sprint(DiaryCap) + ` characters. Reply with only the entry.`

// Days are esn's episode file days (YYYY-MM-DD, the robot's zone), oldest first.
func Days(esn string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(filepath.Dir(EpisodePath(esn, "x")), "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range files {
		day := strings.TrimSuffix(filepath.Base(p), ".jsonl")
		if _, err := time.Parse("2006-01-02", day); err == nil {
			out = append(out, day)
		}
	}
	sort.Strings(out)
	return out, nil
}

// DiaryDue are esn's closed days (before today) with episodes and no diary entry yet — an entry is never rewritten.
func (st *Store) DiaryDue(esn string, days []string, today string) []string {
	have := map[string]bool{}
	for _, e := range st.Diary {
		if e.ESN == esn {
			have[e.Day] = true
		}
	}
	var out []string
	for _, d := range days {
		if d < today && !have[d] {
			out = append(out, d)
		}
	}
	return out
}

// DiaryUser is the diary user message: day's turns, user and robot lines.
func DiaryUser(day string, eps []Episode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Day: %s\n", day)
	for _, e := range eps {
		if u := strings.TrimSpace(e.User); u != "" {
			fmt.Fprintf(&b, "User: %s\n", u)
		}
		if r := strings.TrimSpace(e.Reply); r != "" {
			fmt.Fprintf(&b, "Robot: %s\n", r)
		}
	}
	return b.String()
}

// WriteDiary asks t for esn's entry for day; an over-long reply is cut at a word to DiaryCap runes.
func WriteDiary(ctx context.Context, t llm.Target, esn, day string, now time.Time) (Entry, error) {
	eps, err := ReadJSONL(EpisodePath(esn, day))
	if err != nil {
		return Entry{}, err
	}
	text, err := Ask(ctx, t, DiaryPrompt, DiaryUser(day, eps), DiaryReplyCap)
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: esn + "@" + day, ESN: esn, Day: day, Text: cut(text, DiaryCap), At: now}, nil
}

// AddDiary appends e unless esn already has an entry for its day.
func (st *Store) AddDiary(e Entry) {
	for _, d := range st.Diary {
		if d.ESN == e.ESN && d.Day == e.Day {
			return
		}
	}
	st.Diary = append(st.Diary, e)
}

// cut shortens s to at most n runes, at the last space when there is one.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	out := string(r[:n-1])
	if i := strings.LastIndex(out, " "); i > 0 {
		out = out[:i]
	}
	return strings.TrimRight(out, " ,;") + "…"
}

// Prune deletes esn's episode files older than RetainEpisodes before today and forgets their extracted session ids.
func (st *Store) Prune(esn string, days []string, today time.Time) (gone int, err error) {
	cutoff := today.Add(-RetainEpisodes).Format("2006-01-02")
	for _, d := range days {
		if d < cutoff {
			if err := os.Remove(EpisodePath(esn, d)); err != nil {
				return gone, err
			}
			gone++
		}
	}
	keep := st.Extracted[:0:0]
	for _, id := range st.Extracted {
		if at := strings.TrimPrefix(id, esn+"@"); at == id || len(at) < 10 || at[:10] >= cutoff {
			keep = append(keep, id)
		}
	}
	st.Extracted = keep
	return gone, nil
}
