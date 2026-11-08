package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/llm"
)

// ErrBusy is Run while another pass is in flight.
var ErrBusy = errors.New("memory pass already running")

var runMu sync.Mutex

// ESNs are the robots with captured episodes.
func ESNs() ([]string, error) {
	des, err := os.ReadDir(filepath.Dir(filepath.Dir(EpisodePath("x", "x"))))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []string
	for _, d := range des {
		if d.IsDir() {
			out = append(out, d.Name())
		}
	}
	return out, err
}

// Run is one memory pass over every ESN (M16), single-flight; zone gives an ESN's day boundary. LastRun is set only when
// every ESN's pass finished — a model error leaves the rest for the next due tick.
func Run(ctx context.Context, t llm.Target, zone func(esn string) *time.Location) error {
	if !runMu.TryLock() {
		return ErrBusy
	}
	defer runMu.Unlock()
	esns, err := ESNs()
	if err != nil {
		return err
	}
	for _, esn := range esns {
		if err := runESN(ctx, t, esn, zone(esn)); err != nil {
			logf("myvector: memory pass "+esn+" stopped:", err)
			return err
		}
	}
	now := Now()
	return Update(func(st *Store) error { st.LastRun = now; return nil })
}

// runESN: extract → merge → profile → guard check / revert → diary → prune. The guard baseline is answered against the
// pre-merge store copy, so a pass that changes no active fact makes no guard call.
func runESN(ctx context.Context, t llm.Target, esn string, zone *time.Location) error {
	start, now := time.Now(), Now()
	eps, err := Episodes(esn)
	if err != nil {
		return err
	}
	st, err := Read()
	if err != nil {
		return err
	}
	pending := st.Pending(Sessions(esn, eps, now))
	for _, s := range pending {
		facts, err := Extract(ctx, t, s, now)
		if uerr := Update(func(st *Store) error { st.Admit(s, facts, err); return nil }); uerr != nil {
			return uerr
		}
		if err != nil && !errors.Is(err, ErrParse) {
			return err
		}
	}

	pre, err := Read()
	if err != nil {
		return err
	}
	changed := false
	for _, c := range Categories {
		if err := pre.DropRemoved(esn); err != nil {
			return err
		}
		b := pre.Batch(esn, c)
		if len(b.Cands) == 0 {
			continue
		}
		ops, err := Merge(ctx, t, b)
		if err != nil {
			return err
		}
		if err := Update(func(st *Store) error { changed = st.ApplyMerge(b, ops) || changed; return nil }); err != nil {
			return err
		}
	}
	guard := "-"
	if err := pre.DropRemoved(esn); err != nil {
		return err
	}
	if _, ok := pre.Profiles[esn]; changed || !ok && len(pre.ActiveFacts(esn)) > 0 { // no profile: a Remove dropped it
		if guard, err = check(ctx, t, esn, pre, now); err != nil {
			return err
		}
	}

	days, err := Days(esn)
	if err != nil {
		return err
	}
	today := now.In(zone).Format("2006-01-02")
	for _, day := range pre.DiaryDue(esn, days, today) {
		e, err := WriteDiary(ctx, t, esn, day, now)
		if err != nil {
			return err
		}
		if err := Update(func(st *Store) error { st.AddDiary(e); return nil }); err != nil {
			return err
		}
	}
	var post *Store
	err = Update(func(st *Store) error {
		post = st
		_, err := st.Prune(esn, days, now.In(zone))
		return err
	})
	if err != nil {
		return err
	}
	conf, sup := counts(pre, post, esn)
	logf(fmt.Sprintf("myvector: memory pass %s sessions=%d +%d ~%d guard=%s %dms", esn, len(pending), conf, sup, guard, time.Since(start).Milliseconds()))
	return nil
}

// check rebuilds the profile and runs the guard, every call synced with Removes made meanwhile (M26); a loss after a Remove
// during the guard is re-asked of the synced baseline. A failed guard call reverts with no try (M14); a refused one stops the pass (M2).
func check(ctx context.Context, t llm.Target, esn string, pre *Store, now time.Time) (string, error) {
	mid, err := Read()
	if err != nil {
		return "", err
	}
	from := mid.ActiveFacts(esn)
	text, perr := BuildProfile(ctx, t, from)
	if err := Update(func(st *Store) error { st.SetProfileOf(esn, text, from, perr, now); mid = st; return nil }); err != nil {
		return "", err
	}
	if err := pre.DropRemoved(esn); err != nil {
		return "", err
	}
	gone := removedN(pre, esn)
	before, err := Answers(ctx, t, pre, esn)
	var after map[string]bool
	if err == nil {
		after, err = Answers(ctx, t, mid, esn)
	}
	lost := Lost(before, after)
	if err == nil && len(lost) > 0 {
		if err = pre.DropRemoved(esn); err == nil && removedN(pre, esn) > gone {
			var again map[string]bool
			if again, err = answers(ctx, t, pre, esn, lost); err == nil {
				for _, c := range lost {
					before[c] = again[c]
				}
				lost = Lost(before, after)
			}
		}
	}
	if err != nil {
		logf("myvector: memory guard "+esn+" call failed, reverting:", err)
		lost = []string{"error"}
	}
	snap := pre.Snapshot(esn)
	if uerr := Update(func(st *Store) error { st.Guard(snap, lost, err == nil, now); return nil }); uerr != nil {
		return "", uerr
	}
	if errors.Is(err, ErrRefused) {
		return "", err
	}
	if len(lost) > 0 {
		return "revert " + strings.Join(lost, ","), nil
	}
	return "ok", nil
}

// removedN counts esn's Removed facts in st.
func removedN(st *Store, esn string) (n int) {
	for _, f := range st.Facts {
		if f.ESN == esn && f.Status == Removed {
			n++
		}
	}
	return n
}

// counts are the candidates in pre that post has Active, and pre's active facts post has Superseded.
func counts(pre, post *Store, esn string) (confirmed, superseded int) {
	now := map[string]string{}
	for _, f := range post.Facts {
		now[f.ID] = f.Status
	}
	for _, f := range pre.Facts {
		switch {
		case f.ESN != esn:
		case f.Status == Unconfirmed && now[f.ID] == Active:
			confirmed++
		case f.Status == Active && now[f.ID] == Superseded:
			superseded++
		}
	}
	return confirmed, superseded
}
