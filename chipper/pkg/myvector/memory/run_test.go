package memory

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func utc(string) *time.Location { return time.UTC }

func TestRun(t *testing.T) {
	useEpisodes(t, `{}`)
	logs := useModel(t, `{}`)
	oldNow := Now
	Now = func() time.Time { return time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { Now = oldNow })
	for _, e := range []Episode{
		{At: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Kind: Chat, User: "Mu koer on Muki.", Reply: "Tore!"},
		{At: time.Date(2026, 11, 6, 10, 5, 0, 0, time.UTC), Kind: Chat, User: "Mis kell on?", Reply: "Kümme."},
		{At: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC), Kind: Chat, User: "Tere.", Reply: "Tere!"},
		{At: time.Date(2026, 11, 8, 8, 55, 0, 0, time.UTC), Kind: Chat, User: "Täna on ilus.", Reply: "Jah."}, // today: open, no diary yet
	} {
		if err := Capture(esn, e, time.UTC); err != nil {
			t.Fatal(err)
		}
	}
	tg, users := recordModel(t, func(u string) string {
		switch {
		case strings.HasPrefix(u, "[") && strings.Contains(u, "Muki"):
			return `[{"category": "pets", "fact": "The user has a dog named Muki.", "evidence": "mu koer on Muki", "event_time": ""}]`
		case strings.HasPrefix(u, "["):
			return "[]"
		case strings.HasPrefix(u, "Category:"):
			return "confirm c1"
		case strings.HasPrefix(u, "- ["):
			return "The user has a dog named Muki."
		case strings.Contains(u, Questions["pets"]) && strings.Contains(u, "Muki"):
			return "A dog, Muki."
		case strings.HasPrefix(u, "Day:"):
			return "Today we talked about Muki."
		}
		return Unknown
	})
	if err := Run(context.Background(), tg, utc); err != nil {
		t.Fatal(err)
	}
	if want := 2 + 1 + 1 + 2*len(Categories) + 2; len(*users) != want {
		t.Fatalf("%d model calls, want %d (2 extract, 1 merge, 1 profile, 2×guard, 2 diary)", len(*users), want)
	}
	st, _ := Read()
	if len(st.Diary) != 2 || st.Diary[0].Day != "2026-11-06" || st.Diary[1].Day != "2026-11-07" {
		t.Fatalf("diary %+v, want one entry per day", st.Diary)
	}
	if f := st.ActiveFacts(esn); len(f) != 1 || st.Profiles[esn].Text == "" || !st.LastRun.Equal(Now()) || !strings.HasSuffix(st.LastGuard, " ok") {
		t.Fatalf("facts %+v profile %+v last_run %v last_guard %q", f, st.Profiles, st.LastRun, st.LastGuard)
	}
	if l := (*logs)[len(*logs)-1]; !strings.Contains(l, "memory pass "+esn+" sessions=2 +1 ~0 guard=ok ") {
		t.Fatalf("pass log %q", l)
	}

	n := len(*users)
	if err := Run(context.Background(), tg, utc); err != nil || len(*users) != n {
		t.Fatalf("second Run the same day: err %v, %d new model calls, want none", err, len(*users)-n)
	}
	if st, _ := Read(); len(st.Diary) != 2 {
		t.Fatalf("second Run wrote a diary entry: %+v", st.Diary)
	}
}

func TestRunBusy(t *testing.T) {
	useEpisodes(t, `{}`)
	useModel(t, `{}`)
	Capture(esn, Episode{At: time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC), Kind: Chat, User: "Tere."}, time.UTC)
	in, release := make(chan bool), make(chan bool)
	tg, _ := recordModel(t, func(string) string { in <- true; <-release; return "[]" })
	done := make(chan error)
	go func() { done <- Run(context.Background(), tg, utc) }()
	<-in
	if err := Run(context.Background(), tg, utc); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent Run = %v, want ErrBusy", err)
	}
	close(release)
	go func() {
		for range in {
		}
	}()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPrune(t *testing.T) {
	useEpisodes(t, `{}`)
	today := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	var days []string
	for _, age := range []int{181, 180, 179} {
		at := today.AddDate(0, 0, -age)
		Capture(esn, Episode{At: at, Kind: Chat, User: "x"}, time.UTC)
		days = append(days, at.Format("2006-01-02"))
	}
	old := esn + "@" + today.AddDate(0, 0, -182).Format("2006-01-02") + "T10:00:00Z"
	st := (&Store{Extracted: []string{old, esn + "@" + days[0] + "T10:00:00Z", esn + "@" + days[2] + "T10:00:00Z", "other@2020-01-01T00:00:00Z"}}).init()
	gone, err := st.Prune(esn, days, today)
	if err != nil || gone != 1 {
		t.Fatalf("Prune = %d, %v; want only the 181-day file", gone, err)
	}
	for i, d := range days {
		if _, err := os.Stat(EpisodePath(esn, d)); (err == nil) != (i > 0) {
			t.Fatalf("day %s (age %d) exists=%v", d, []int{181, 180, 179}[i], err == nil)
		}
	}
	if len(st.Extracted) != 3 || st.Extracted[0] != esn+"@"+days[0]+"T10:00:00Z" {
		t.Fatalf("extracted %v; only ids older than the cutoff day − (24h + SessionGap) go, other ESNs stay", st.Extracted)
	}
	if got := cut(strings.Repeat("sõna ", 100), DiaryCap); len([]rune(got)) > DiaryCap || !strings.HasSuffix(got, "sõna…") {
		t.Fatalf("cut = %q (%d runes)", got, len([]rune(got)))
	}
}

func TestRunRebuildsRemovedProfile(t *testing.T) {
	useEpisodes(t, `{}`)
	useModel(t, `{}`)
	oldNow := Now
	Now = func() time.Time { return time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { Now = oldNow })
	if err := Capture(esn, Episode{At: Now(), Kind: Chat, User: "Tere.", Reply: "Tere!"}, time.UTC); err != nil { // today: open, no diary
		t.Fatal(err)
	}
	t0 := Now().Add(-48 * time.Hour)
	if err := Update(func(st *Store) error {
		st.Facts = []Fact{fact("f1", "The user's dog is called Muki.", "mu koer Muki", t0), fact("f2", "The user lives in Tartu.", "elan Tartus", t0)}
		st.Profiles[esn] = Profile{Text: "Lives in Tartu, has a dog Muki."}
		st.Remove("f1", Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tg, users := recordModel(t, func(u string) string {
		if strings.HasPrefix(u, "- [") {
			return "Lives in Tartu."
		}
		return Unknown
	})
	if err := Run(context.Background(), tg, utc); err != nil {
		t.Fatal(err)
	}
	if want := 1 + 2*len(Categories); len(*users) != want {
		t.Fatalf("%d model calls, want %d (1 profile, 2×guard)", len(*users), want)
	}
	if st, _ := Read(); st.Profiles[esn].Text != "Lives in Tartu." {
		t.Fatalf("profile %+v, want rebuilt without the removed fact", st.Profiles)
	}
}

func TestPruneKeepsLocalMidnightSession(t *testing.T) {
	useEpisodes(t, `{}`)
	tallinn := time.FixedZone("Europe/Tallinn", 2*60*60)
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, tallinn)
	d := now.Add(-RetainEpisodes)
	at := time.Date(d.Year(), d.Month(), d.Day(), 0, 30, 0, 0, tallinn) // the cutoff day, 22:30 UTC the day before
	if err := Capture(esn, Episode{At: at, Kind: Chat, User: "Mu koer on Muki."}, tallinn); err != nil {
		t.Fatal(err)
	}
	eps, _ := Episodes(esn)
	s := Sessions(esn, eps, now)
	st := (&Store{}).init()
	st.Admit(s[0], nil, nil)
	days, _ := Days(esn)
	if gone, err := st.Prune(esn, days, now); err != nil || gone != 0 {
		t.Fatalf("Prune = %d, %v; the cutoff day's file stays", gone, err)
	}
	if p := st.Pending(Sessions(esn, eps, now)); len(p) != 0 {
		t.Fatalf("kept file's session %s pending again: extracted %v", p[0].ID, st.Extracted)
	}
}

func TestRunRemoveMidPass(t *testing.T) {
	for _, during := range []string{"Category: family", "- ["} { // the merge call, the profile call
		t.Run(during, func(t *testing.T) {
			useEpisodes(t, `{}`)
			useModel(t, `{}`)
			oldNow := Now
			Now = func() time.Time { return time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC) }
			t.Cleanup(func() { Now = oldNow })
			if err := Capture(esn, Episode{At: Now(), Kind: Chat, User: "Tere.", Reply: "Tere!"}, time.UTC); err != nil {
				t.Fatal(err)
			}
			if err := Update(func(st *Store) error {
				st.Facts = []Fact{mfact("c1", "family", Unconfirmed, "The user has a sister Mari."), mfact("f1", "pets", Active, "The user has a dog named Muki."),
					mfact("c2", "pets", Unconfirmed, "The user has a cat."), mfact("c3", "home", Unconfirmed, "The user lives in Tartu.")}
				st.Profiles[esn] = Profile{Text: "Has a dog Muki.", From: []string{"f1"}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			calls, removedAt := 0, -1
			tg, users := recordModel(t, func(u string) string {
				if calls++; removedAt < 0 && strings.HasPrefix(u, during) {
					Update(func(st *Store) error { st.Remove("f1", Now()); return nil }) // the user deletes Muki mid-call
					removedAt = calls
				}
				switch {
				case strings.HasPrefix(u, "Category:"):
					return "confirm c1"
				case strings.HasPrefix(u, "- ["):
					return strings.ReplaceAll(strings.TrimSpace(u), "\n", " ")
				case strings.Contains(u, "Muki") && strings.Contains(u, Questions["pets"]):
					return "A dog, Muki."
				case strings.Contains(u, "Tartu") && strings.Contains(u, Questions["home"]):
					return "Tartu."
				}
				return Unknown
			})
			for run := 1; run <= 2; run++ {
				if err := Run(context.Background(), tg, utc); err != nil {
					t.Fatal(err)
				}
				st, _ := Read()
				if p := st.Profiles[esn]; strings.Contains(p.Text, "Muki") || run == 2 && !strings.Contains(p.Text, "Tartu") || !strings.HasSuffix(st.LastGuard, " ok") {
					t.Fatalf("run %d: profile %+v last_guard %q; want built without the removed fact, no revert", run, p, st.LastGuard)
				}
				if s := status(st); s["c1"] != Active || s["c3"] != Active || s["f1"] != Removed {
					t.Fatalf("run %d: %v", run, s)
				}
			}
			for _, u := range (*users)[removedAt:] {
				if strings.Contains(u, "Muki") {
					t.Fatalf("a model call after the Remove still sent the removed fact:\n%s", u)
				}
			}
		})
	}
}

func TestRunStopsWhenModelSwitchedOff(t *testing.T) {
	useEpisodes(t, `{}`)
	useModel(t, `{}`)
	var off atomic.Bool
	configBytes = func() ([]byte, error) {
		if off.Load() {
			return []byte(`{"memory_model":"off"}`), nil
		}
		return []byte(`{}`), nil
	}
	for _, at := range []time.Time{t0, t0.Add(time.Hour)} {
		Capture(esn, Episode{At: at, Kind: Chat, User: "Mu koer on Muki."}, time.UTC)
	}
	tg, users := recordModel(t, func(string) string { off.Store(true); return "[]" })
	if err := Run(context.Background(), tg, utc); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("Run = %v, want the refused call's error", err)
	}
	if len(*users) != 1 {
		t.Fatalf("%d model calls, want 1: none after memory_model turned off", len(*users))
	}
}
