package processreqs

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/usage"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
	"google.golang.org/grpc/peer"
)

// forkPeerStream is a chipper intent stream whose context carries a gRPC peer.
type forkPeerStream struct {
	pb.ChipperGrpc_StreamingIntentServer
	ctx context.Context
}

func (f *forkPeerStream) Context() context.Context { return f.ctx }

func forkStreamFrom(ip string) *forkPeerStream {
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 51000}})
	return &forkPeerStream{ctx: ctx}
}

// forkUsageRun counts one request per (stream, transcript) through forkUsageCount into a temp ledger; returns its rows.
func forkUsageRun(t *testing.T, reqs []struct {
	stream interface{}
	text   string
	err    error
}) []usage.Row {
	t.Helper()
	l := usage.Open(filepath.Join(t.TempDir(), "usage.tsv"))
	oldL, oldNow := forkUsageLedger, forkUsageNow
	forkUsageLedger, forkUsageNow = l, func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { forkUsageLedger, forkUsageNow = oldL, oldNow })
	for _, r := range reqs {
		calls := 0
		h := forkUsageCount(func(sr.SpeechRequest) (string, error) { calls++; return r.text, r.err })
		text, err := h(sr.SpeechRequest{Stream: r.stream})
		if calls != 1 || text != r.text || err != r.err {
			t.Fatalf("wrapped handler: calls=%d text=%q err=%v, want 1 call passing %q/%v through", calls, text, err, r.text, r.err)
		}
	}
	rows, err := l.Rows()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestForkUsageCountsLoopbackAsTest(t *testing.T) {
	got := forkUsageRun(t, []struct {
		stream interface{}
		text   string
		err    error
	}{
		{forkStreamFrom("127.0.0.1"), "what time is it", nil}, // fakebot / live-check, robot's ESN
		{forkStreamFrom("192.168.1.50"), "what time is it", nil},
	})
	want := []usage.Row{{Day: "2026-11-20", Real: 1, Test: 1, Tailnet: "-"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func TestForkUsageEmptyTranscriptCountsNowhere(t *testing.T) {
	got := forkUsageRun(t, []struct {
		stream interface{}
		text   string
		err    error
	}{
		{forkStreamFrom("192.168.1.50"), "  ", nil},
		{forkStreamFrom("192.168.1.50"), "", errors.New("STT unavailable")},
	})
	if len(got) != 0 {
		t.Errorf("a false wake / STT error was counted: %+v", got)
	}
}

func TestForkUsageUnknownPeerIsNotReal(t *testing.T) {
	got := forkUsageRun(t, []struct {
		stream interface{}
		text   string
		err    error
	}{
		{nil, "hello", nil},
		{&forkFakeIntent{}, "hello", nil}, // embedded nil stream: Context() panics, recovered
		{&forkPeerStream{ctx: context.Background()}, "hello", nil},
	})
	want := []usage.Row{{Day: "2026-11-20", Test: 3, Tailnet: "-"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func TestForkUsageLedgerFailureOnlyLogs(t *testing.T) {
	oldL := forkUsageLedger
	forkUsageLedger = usage.Open(filepath.Join(t.TempDir(), "missing-dir", "usage.tsv"))
	t.Cleanup(func() { forkUsageLedger = oldL })
	h := forkUsageCount(func(sr.SpeechRequest) (string, error) { return "hello", nil })
	if text, err := h(sr.SpeechRequest{Stream: forkStreamFrom("192.168.1.50")}); text != "hello" || err != nil {
		t.Errorf("a ledger write failure changed the request: %q, %v", text, err)
	}
}
