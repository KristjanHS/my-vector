package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func init() { pace = 0 }

// fakeChipper records every request, then replies with one intent once the client closes.
type fakeChipper struct {
	pb.UnimplementedChipperGrpcServer
	reqs []*pb.StreamingIntentGraphRequest
}

func (f *fakeChipper) StreamingIntentGraph(s pb.ChipperGrpc_StreamingIntentGraphServer) error {
	for {
		r, err := s.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		f.reqs = append(f.reqs, r)
	}
	return s.Send(&pb.IntentGraphResponse{ResponseType: pb.IntentGraphMode_INTENT, IsFinal: true,
		IntentResult: &pb.IntentResult{Action: "intent_clock_time", QueryText: "what time is it"}})
}

func stream(t *testing.T, pcm []byte) (*fakeChipper, []*pb.IntentGraphResponse) {
	t.Helper()
	lis, f := bufconn.Listen(1<<20), &fakeChipper{}
	srv := grpc.NewServer()
	pb.RegisterChipperGrpcServer(srv, f)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)
	conn, err := grpc.Dial("bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	rs, err := run(context.Background(), pb.NewChipperGrpcClient(conn), "00e1test", pcm)
	if err != nil {
		t.Fatal(err)
	}
	return f, rs
}

func TestRunStreamsAsVicCloud(t *testing.T) {
	pcm := bytes.Repeat([]byte{1, 2}, 5000) // 10000 B: not a chunk multiple
	f, rs := stream(t, pcm)
	if len(f.reqs) == 0 {
		t.Fatal("no requests")
	}
	first := f.reqs[0]
	if first.DeviceId != "00e1test" || len(first.Session) != 16 || first.LanguageCode != pb.LanguageCode_ENGLISH_US ||
		first.AudioEncoding != pb.AudioEncoding_LINEAR_PCM || first.Mode != pb.RobotMode_VOICE_COMMAND {
		t.Fatalf("first request fields: %+v", first)
	}
	var got []byte
	for i, r := range f.reqs {
		if len(r.InputAudio) > 3200 {
			t.Fatalf("req %d: %d B > 3200 (100 ms)", i, len(r.InputAudio))
		}
		if i > 0 && r.DeviceId != "" {
			t.Fatalf("req %d repeats DeviceId", i)
		}
		got = append(got, r.InputAudio...)
	}
	want := append(append(make([]byte, 3200), pcm...), make([]byte, 32000)...)
	if !bytes.Equal(got, want) {
		t.Fatalf("reassembled audio: %d B, want %d B (100 ms zeros + input + 1 s zeros)", len(got), len(want))
	}
	if len(rs) != 1 || rs[0].GetIntentResult().GetAction() != "intent_clock_time" {
		t.Fatalf("replies: %v", rs)
	}
}

func TestRunPadsOpusMagic(t *testing.T) {
	f, _ := stream(t, []byte{0x4f, 0x67, 0x67, 0x53})
	if a := f.reqs[0].InputAudio; len(a) == 0 || a[0] == 0x4f {
		t.Fatalf("first InputAudio is %d B starting 0x4f: server would sniff Opus", len(a))
	}
}

func TestVerdict(t *testing.T) {
	rs := []*pb.IntentGraphResponse{{IntentResult: &pb.IntentResult{Action: "intent_clock_time"}}}
	for _, c := range []struct {
		rs   []*pb.IntentGraphResponse
		want string
		code int
	}{{rs, "", 0}, {rs, "intent_clock_time", 0}, {rs, "x", 1}, {nil, "", 1}} {
		if got := verdict(c.rs, c.want); got != c.code {
			t.Errorf("verdict(%d replies, %q) = %d, want %d", len(c.rs), c.want, got, c.code)
		}
	}
}

func TestDescribe(t *testing.T) {
	kg := describe(&pb.IntentGraphResponse{ResponseType: pb.IntentGraphMode_KNOWLEDGE_GRAPH, SpokenText: "Paris", QueryText: "capital of france"})
	if len(kg) != 2 || kg[0] != `kg "Paris"` || kg[1] != `query "capital of france"` {
		t.Fatalf("kg lines: %q", kg)
	}
}
