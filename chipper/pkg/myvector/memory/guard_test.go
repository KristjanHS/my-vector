package memory

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestGuardRevert(t *testing.T) {
	logs := useModel(t, `{}`)
	after := false
	tg, users := recordModel(t, func(u string) string {
		switch {
		case strings.Contains(u, Questions["identity"]) && after:
			return "UNKNOWN."
		case strings.Contains(u, Questions["identity"]):
			return "Kalle"
		case strings.Contains(u, Questions["pets"]) && after:
			return "A dog named Muki."
		case strings.Contains(u, Questions["pets"]):
			return "A cat named Miisu."
		case strings.Contains(u, Questions["work"]) && after:
			return "Librarian." // gained, never lost
		}
		return "unknown"
	})
	st := (&Store{Facts: []Fact{
		mfact("i1", "identity", Active, "The user's name is Kalle."),
		mfact("p1", "pets", Active, "The user has a cat named Miisu."),
		mfact("pc", "pets", Unconfirmed, "The user has a dog named Muki."),
		mfact("ic", "identity", Unconfirmed, "The user is called K."),
		mfact("gone", "pets", Active, "The user has a hamster."),
		{ID: "o1", ESN: "other", Category: "pets", Text: "x", Status: Active},
	}}).init()
	st.Profiles[esn] = Profile{Text: "The user is Kalle; cat Miisu.", At: t0}
	before, err := Answers(context.Background(), tg, st, esn)
	if err != nil || !before["identity"] || !before["pets"] || before["work"] || len(*users) != len(Categories) {
		t.Fatalf("baseline %v %v, %d calls", before, err, len(*users))
	}
	snap := st.Snapshot(esn)

	b := st.Batches(esn)
	st.ApplyMerge(b[0], []Op{{Kind: Supersede, Old: "i1", ID: "ic"}})
	st.ApplyMerge(b[1], []Op{{Kind: Supersede, Old: "p1", ID: "pc"}})
	st.SetProfile(esn, "A dog named Muki.", nil, t0.Add(time.Hour))
	now := t0.Add(2 * time.Hour)
	st.Facts[4].Status, st.Facts[4].RemovedAt = Removed, &now // the user deletes it mid-pass
	after = true
	got, err := Answers(context.Background(), tg, st, esn)
	if err != nil {
		t.Fatal(err)
	}
	lost := Lost(before, got)
	if len(lost) != 1 || lost[0] != "identity" {
		t.Fatalf("lost = %v; pets changed but is still known, only identity is lost", lost)
	}
	st.Guard(snap, lost, now)

	st.Facts[len(st.Facts)-1].Status, st.Facts[len(st.Facts)-1].RemovedAt = Active, nil // compare the rest to snap
	if again := st.Snapshot(esn); !bytes.Equal(again.Facts, snap.Facts) || st.Profiles[esn] != snap.Profile {
		t.Fatalf("revert: facts\n%s\nwant\n%s\nprofile %+v", again.Facts, snap.Facts, st.Profiles[esn])
	}
	if s := status(st); s["pc"] != Unconfirmed || s["ic"] != Unconfirmed || s["o1"] != Active {
		t.Fatalf("candidates must be unconfirmed again, other ESN untouched: %v", s)
	}
	if !strings.HasSuffix(st.LastGuard, " revert identity") || !strings.HasPrefix(st.LastGuard, esn+" ") || len(*logs) != 1 {
		t.Fatalf("last_guard %q logs %q", st.LastGuard, *logs)
	}

	st2 := (&Store{Facts: []Fact{mfact("gone", "pets", Active, "h")}}).init()
	snap2 := st2.Snapshot(esn)
	st2.Facts[0].Status, st2.Facts[0].RemovedAt = Removed, &now
	st2.Guard(snap2, []string{"pets"}, now)
	if st2.Facts[0].Status != Removed || st2.Facts[0].RemovedAt == nil {
		t.Fatal("a revert must not bring back a fact the user removed during the pass")
	}
	st2.Guard(snap2, nil, now)
	if !strings.HasSuffix(st2.LastGuard, " ok") {
		t.Fatalf("no loss → last_guard ok, got %q", st2.LastGuard)
	}
}
