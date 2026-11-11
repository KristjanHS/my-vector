package wirepod_ttr

import (
	"bytes"
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/memory"
	"google.golang.org/grpc"
)

// nativeBot is a photoBot whose gallery holds ids and gains next on the second PhotosInfo (-1 = never); its first
// fails Photo calls answer Success false.
type nativeBot struct {
	*photoBot
	mu      sync.Mutex
	ids     []uint32
	next    int
	fails   int
	infos   int
	bare    int // calls whose ctx has no deadline
	fetched []uint32
}

var nativeJPEG = []byte{0xff, 0xd8, 0xff, 0x42}

func (b *nativeBot) PhotosInfo(ctx context.Context, _ *vectorpb.PhotosInfoRequest, _ ...grpc.CallOption) (*vectorpb.PhotosInfoResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		b.bare++
	}
	if b.infos++; b.infos == 2 && b.next >= 0 {
		b.ids = append(b.ids, uint32(b.next))
	}
	var out []*vectorpb.PhotoInfo
	for _, id := range b.ids {
		out = append(out, &vectorpb.PhotoInfo{PhotoId: id})
	}
	return &vectorpb.PhotosInfoResponse{PhotoInfos: out}, nil
}

func (b *nativeBot) Photo(ctx context.Context, in *vectorpb.PhotoRequest, _ ...grpc.CallOption) (*vectorpb.PhotoResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := ctx.Deadline(); !ok {
		b.bare++
	}
	b.fetched = append(b.fetched, in.PhotoId)
	if len(b.fetched) <= b.fails {
		return &vectorpb.PhotoResponse{Success: false, Image: nativeJPEG}, nil
	}
	return &vectorpb.PhotoResponse{Success: true, Image: nativeJPEG}, nil
}

// useNativeBot points the photo rig's robot at a nativeBot and shortens the N1 poll.
func useNativeBot(t *testing.T, r *photoRig, ids []uint32, next int) *nativeBot {
	b := &nativeBot{photoBot: r.bot, ids: ids, next: next}
	oldWait, oldEvery, oldRobot := forkNativePhotoWait, forkNativePhotoEvery, forkPhotoRobot
	forkNativePhotoWait, forkNativePhotoEvery = 200*time.Millisecond, time.Millisecond
	forkPhotoRobot = func(esn string) (*vector.Vector, error) {
		v := &vector.Vector{Conn: b}
		v.Cfg.SerialNo = esn
		return v, nil
	}
	t.Cleanup(func() { forkNativePhotoWait, forkNativePhotoEvery, forkPhotoRobot = oldWait, oldEvery, oldRobot })
	return b
}

// N1 + N2 + F3/F4: the first id newer than the pre-shot newest is fetched once and kept fresh; a follow-up naming the
// picture is answered from its bytes with no capture.
func TestForkNativePhotoKept(t *testing.T) {
	r := usePhotoRig(t)
	b := useNativeBot(t, r, []uint32{7, 3}, 9)
	forkNativePhoto(followESN, lang.Estonian)
	if len(b.fetched) != 1 || b.fetched[0] != 9 {
		t.Fatalf("fetched %v, want [9]", b.fetched)
	}
	if b.bare != 0 {
		t.Errorf("%d robot calls without a deadline", b.bare)
	}
	if s, ok := forkPhotos.Fresh(followESN, forkNow()); !ok || !bytes.Equal(s.JPEG, nativeJPEG) || s.Lang != lang.Estonian {
		t.Fatalf("store fresh %v, shot %v %q", ok, s.JPEG, s.Lang)
	}
	if forkPhotos.TakeTurn(followESN) {
		t.Fatal("native photo marked answered this turn: an unrelated turn would reopen the mic (N3)")
	}
	if !forkPhotoTurn(nil, followESN, false, "mis pildi peal on?") {
		t.Fatal("follow-up on the native photo not handled")
	}
	if c, _ := r.counts(); c != 0 {
		t.Errorf("captures %d, want 0", c)
	}
	if v := r.visionBodies(); len(v) != 1 || !strings.Contains(v[0], base64.StdEncoding.EncodeToString(nativeJPEG)) {
		t.Errorf("vision chats %d, want 1 carrying the native photo", len(v))
	}
}

// N1: no newer id within the wait → nothing fetched, store untouched.
func TestForkNativePhotoNone(t *testing.T) {
	r := usePhotoRig(t)
	b := useNativeBot(t, r, []uint32{7}, -1)
	forkNativePhoto(followESN, lang.Estonian)
	if len(b.fetched) != 0 || b.infos < 3 {
		t.Errorf("fetched %v after %d polls, want none after several", b.fetched, b.infos)
	}
	if _, ok := forkPhotos.Fresh(followESN, forkNow()); ok {
		t.Error("store fresh with no new photo")
	}
}

// N1: an empty gallery's first photo is id 0 and still counts as new.
func TestForkNativePhotoFirst(t *testing.T) {
	r := usePhotoRig(t)
	b := useNativeBot(t, r, nil, 0)
	forkNativePhoto(followESN, lang.Estonian)
	if len(b.fetched) != 1 || b.fetched[0] != 0 {
		t.Fatalf("fetched %v, want [0]", b.fetched)
	}
	if _, ok := forkPhotos.Fresh(followESN, forkNow()); !ok {
		t.Error("store not fresh after the gallery's first photo")
	}
}

// N1: a Success false fetch is retried on the next poll.
func TestForkNativePhotoRetry(t *testing.T) {
	r := usePhotoRig(t)
	b := useNativeBot(t, r, []uint32{4}, 5)
	b.fails = 1
	forkNativePhoto(followESN, lang.Estonian)
	if len(b.fetched) != 2 || b.fetched[1] != 5 {
		t.Fatalf("fetched %v, want [5 5]", b.fetched)
	}
	if _, ok := forkPhotos.Fresh(followESN, forkNow()); !ok {
		t.Error("store not fresh after the retried fetch")
	}
}

// N1: forkIntentPass starts the poll for intent_photo_take_extend only.
func TestForkNativePhotoIntentPass(t *testing.T) {
	r := usePhotoRig(t)
	old, dir := memory.Path, t.TempDir()
	memory.Path = func() string { return filepath.Join(dir, "myvectorMemory.json") }
	t.Cleanup(func() { memory.Path = old })
	b := useNativeBot(t, r, nil, 1)
	forkIntentPass(nil, followESN, "intent_clock_time", "mis kell on")
	time.Sleep(20 * time.Millisecond)
	if b.mu.Lock(); b.infos != 0 {
		t.Errorf("clock intent polled PhotosInfo %d times", b.infos)
	}
	b.mu.Unlock()
	forkIntentPass(nil, followESN, "intent_photo_take_extend", "pildista")
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if _, ok := forkPhotos.Fresh(followESN, forkNow()); ok {
			return
		}
	}
	t.Error("photo intent: no native photo kept within 1 s")
}
