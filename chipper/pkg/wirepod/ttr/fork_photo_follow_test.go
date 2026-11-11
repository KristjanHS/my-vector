package wirepod_ttr

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
)

const followESN = "00e60001"

// photoAnswered runs a phrase photo turn to its remembered answer.
func photoAnswered(t *testing.T, r *photoRig) {
	t.Helper()
	if !forkPhotoTurn(nil, followESN, true, "Mida sa näed laual?") {
		t.Fatal("photo phrase not handled")
	}
	history(followESN, 2)
}

func (r *photoRig) counts() (captures, fires int) {
	r.bot.smu.Lock()
	captures = r.bot.captures
	r.bot.smu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	return captures, r.fires
}

// visionBodies are the fake's vision chat bodies.
func (r *photoRig) visionBodies() []string {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	var out []string
	for _, b := range r.f.bodies {
		if !strings.Contains(b, `"model":"estllm:x"`) {
			out = append(out, b)
		}
	}
	return out
}

// F6 + F3/F4: a photo answer with no "?" still listens again; an F3 follow-up and a no-F3 utterance heard in the
// photo-armed listen each reuse the kept photo — "Vaatan.", one vision chat with the stored bytes, zero captures.
func TestForkPhotoFollowReuse(t *testing.T) {
	r := usePhotoRig(t)
	photoAnswered(t, r)
	if c, f := r.counts(); c != 1 || f != 1 {
		t.Fatalf("photo turn: captures %d, listens %d, want 1 and 1", c, f)
	}
	kept := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 1}) // capture 1's bytes, not a re-capture's
	for i, ask := range []struct {
		text  string
		armed bool
	}{{"kus on tass?", true}, {"kus pildi peal on tass?", false}} {
		isKG := ask.armed
		if isKG {
			if _, skip := forkBegin(nil, followESN, true, ask.text); skip {
				t.Fatal("armed follow-up skipped")
			}
		}
		if !forkPhotoTurn(nil, followESN, isKG, ask.text) {
			t.Fatalf("%q: not handled as a kept-photo ask", ask.text)
		}
		if c, _ := r.counts(); c != 1 {
			t.Errorf("%q: captures %d, want the first photo's 1 only", ask.text, c)
		}
		v := r.visionBodies()
		if len(v) != 2+i || !strings.Contains(v[len(v)-1], kept) {
			t.Errorf("%q: vision chats %d, want %d, the last carrying the kept JPEG %s", ask.text, len(v), 2+i, kept)
		}
		if brain, _ := r.chats(); !strings.Contains(strings.Join(brain, "\n"), ask.text) {
			t.Errorf("%q: no relay hop 0 on the follow-up text", ask.text)
		}
	}
	r.bot.smu.Lock()
	if n := strings.Count(strings.Join(r.bot.said, ","), "3"); n != 1 {
		t.Errorf("countdowns %d (said %q), want the first photo's 1", n, r.bot.said)
	}
	r.bot.smu.Unlock()
	if got := strings.Count(strings.Join(r.lines, "|"), "Vaatan."); got != 3 {
		t.Errorf("Vaatan. lines %d, want 3 (%q)", got, r.lines)
	}
}

// F2 / F3: no reuse without a fresh photo, nor for an utterance with no F3 word outside a photo-armed listen; a photo
// phrase while fresh takes a new photo.
func TestForkPhotoFollowFallsThrough(t *testing.T) {
	r := usePhotoRig(t)
	if forkPhotoTurn(nil, followESN, false, "kus pildi peal on tass?") {
		t.Fatal("F3 word with no photo handled")
	}
	photoAnswered(t, r)
	if forkPhotoTurn(nil, followESN, false, "kus on tass?") {
		t.Error("wake-word utterance without an F3 word reused the photo")
	}
	old := forkNow
	t.Cleanup(func() { forkNow = old })
	forkNow = func() time.Time { return old().Add(121 * time.Second) }
	if forkPhotoTurn(nil, followESN, false, "kus pildi peal on tass?") {
		t.Error("stale photo reused")
	}
	forkNow = old
	if !forkPhotoTurn(nil, followESN, false, "mida sa näed?") {
		t.Fatal("photo phrase not handled")
	}
	if c, _ := r.counts(); c != 2 {
		t.Errorf("captures %d, want 2: a photo phrase takes a new photo", c)
	}
}

// F7: in a photo-armed listen "tee uus pilt" takes a new photo, its bytes in the vision chat.
func TestForkPhotoFollowNew(t *testing.T) {
	r := usePhotoRig(t)
	photoAnswered(t, r)
	if _, skip := forkBegin(nil, followESN, true, "tee uus pilt"); skip {
		t.Fatal("armed request skipped")
	}
	if !forkPhotoTurn(nil, followESN, true, "tee uus pilt") {
		t.Fatal("new-photo ask not handled")
	}
	if c, _ := r.counts(); c != 2 {
		t.Errorf("captures %d, want 2: a new-photo ask beats the kept photo", c)
	}
	fresh := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 2})
	if v := r.visionBodies(); len(v) != 2 || !strings.Contains(v[1], fresh) {
		t.Errorf("vision chats %d, want 2, the last carrying capture 2's JPEG %s", len(v), fresh)
	}
}

// F6: a non-photo arm clears the photo listen; a failed capture never listens; a brain-photo answer forces the
// decider's NO to a listen.
func TestForkPhotoFollowListen(t *testing.T) {
	r := usePhotoRig(t)
	photoAnswered(t, r)
	no, yes := false, true
	if !forkFollowUp(nil, decided(&yes), followESN, false) {
		t.Fatal("decider YES did not arm")
	}
	if _, skip := forkBegin(nil, followESN, true, "kus on tass?"); skip || forkPhotoTurn(nil, followESN, true, "kus on tass?") {
		t.Error("a decider-armed listen reused the photo without an F3 word")
	}

	forkPhotos.Answered(followESN, forkNow()) // the brain's getImage answer this turn
	if !forkFollowUp(nil, decided(&no), followESN, false) {
		t.Error("photo answered this turn: decider NO kept the mic closed")
	}

	r2 := usePhotoRig(t)
	r2.bot.noPhoto = true
	if !forkPhotoTurn(nil, followESN, true, "mida sa näed?") {
		t.Fatal("photo phrase not handled")
	}
	if _, f := r2.counts(); f != 0 {
		t.Errorf("failed capture listened again (%d)", f)
	}
}

// F6: an intent-graph answer during a photo-armed listen ends the chain and the photo listen, so the next knowledge
// request is no follow-up and does not reuse the photo; a skipped request ends the photo listen too.
func TestForkPhotoFollowIntentEnds(t *testing.T) {
	r := usePhotoRig(t)
	old, dir := memory.Path, t.TempDir()
	memory.Path = func() string { return filepath.Join(dir, "myvectorMemory.json") }
	t.Cleanup(func() { memory.Path = old })
	photoAnswered(t, r)
	if _, f := r.counts(); f != 1 {
		t.Fatalf("photo turn listens %d, want 1", f)
	}
	forkIntentPass(nil, followESN, "intent_clock_time", "mis kell on")
	if forkPhotos.TakeListen(followESN) {
		t.Error("intent-graph answer left the photo listen set")
	}
	forkPhotos.Listen(followESN, true) // re-set, so the next check fails on a surviving chain alone
	if _, skip := forkBegin(nil, followESN, true, "kus on tass?"); skip || forkChain.Count(followESN) != 0 || forkPhotoTurn(nil, followESN, true, "kus on tass?") {
		t.Errorf("after an intent-graph answer: skip %v, chain %d, or the photo was reused", skip, forkChain.Count(followESN))
	}

	forkPhotos.Listen(followESN, true)
	if _, skip := forkBegin(nil, followESN, true, " "); !skip || forkPhotos.TakeListen(followESN) {
		t.Errorf("blank request: skip %v, or its photo listen survived", skip)
	}
}
