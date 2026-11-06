package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func init() { pace = 0 }

// fakeChipper records every request, then replies with one intent once the client closes;
// hold keeps the stream open after the reply, as wire-pod does while the LLM speaks.
type fakeChipper struct {
	pb.UnimplementedChipperGrpcServer
	reqs []*pb.StreamingIntentGraphRequest
	hold bool
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
	err := s.Send(&pb.IntentGraphResponse{ResponseType: pb.IntentGraphMode_INTENT, IsFinal: true,
		IntentResult: &pb.IntentResult{Action: "intent_clock_time", QueryText: "what time is it"}})
	if err == nil && f.hold {
		<-s.Context().Done()
	}
	return err
}

func stream(t *testing.T, pcm []byte) (*fakeChipper, *pb.IntentGraphResponse) {
	t.Helper()
	return streamTo(t, &fakeChipper{}, pcm)
}

func streamTo(t *testing.T, f *fakeChipper, pcm []byte) (*fakeChipper, *pb.IntentGraphResponse) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := run(ctx, pb.NewChipperGrpcClient(conn), "00e1test", pcm)
	if err != nil {
		t.Fatal(err)
	}
	return f, r
}

func TestRunStreamsAsVicCloud(t *testing.T) {
	pcm := bytes.Repeat([]byte{1, 2}, 5000) // 10000 B: not a chunk multiple
	f, r := stream(t, pcm)
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
	if r.GetIntentResult().GetAction() != "intent_clock_time" {
		t.Fatalf("reply: %v", r)
	}
}

func TestRunReturnsOnFirstReply(t *testing.T) {
	if _, r := streamTo(t, &fakeChipper{hold: true}, []byte{1, 2}); r == nil {
		t.Fatal("no reply")
	}
}

func TestRunPadsOpusMagic(t *testing.T) {
	f, _ := stream(t, []byte{0x4f, 0x67, 0x67, 0x53})
	if a := f.reqs[0].InputAudio; len(a) == 0 || a[0] == 0x4f {
		t.Fatalf("first InputAudio is %d B starting 0x4f: server would sniff Opus", len(a))
	}
}

func TestVerdict(t *testing.T) {
	r := &pb.IntentGraphResponse{IntentResult: &pb.IntentResult{Action: "intent_clock_time"}}
	for _, c := range []struct {
		r    *pb.IntentGraphResponse
		want string
		code int
	}{{r, "", 0}, {r, "intent_clock_time", 0}, {r, "x", 1}, {nil, "", 1}, {nil, "x", 1}} {
		if got := verdict(c.r, c.want); got != c.code {
			t.Errorf("verdict(reply %t, %q) = %d, want %d", c.r != nil, c.want, got, c.code)
		}
	}
}

func TestSynthPostsAfBella(t *testing.T) {
	pcm := bytes.Repeat([]byte{7, 0}, 4000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || r.URL.Path != "/tts" ||
			body["text"] != "what time is it" || body["voice"] != "af_bella" || body["lang"] != "en-US" || len(body) != 3 {
			t.Errorf("%s %s %v", r.Method, r.URL.Path, body)
		}
		w.Write(pcm)
	}))
	defer ts.Close()
	got, err := synth(ts.URL, "what time is it", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := stream(t, got)
	var streamed []byte
	for _, r := range f.reqs {
		streamed = append(streamed, r.InputAudio...)
	}
	if !bytes.Equal(streamed[3200:3200+len(pcm)], pcm) {
		t.Fatal("streamed audio does not carry the TTS PCM")
	}
}

func TestSynthServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()
	if _, err := synth(ts.URL, "x", "en-US"); err == nil {
		t.Fatal("TTS 500: want error")
	}
}

func TestSynthUnknownLang(t *testing.T) {
	_, err := synth("http://127.0.0.1:1", "x", "de-DE")
	if err == nil || !strings.Contains(err.Error(), "no voice for -lang") {
		t.Fatalf("de-DE: err = %v, want no voice for -lang", err)
	}
}

func TestDescribe(t *testing.T) {
	kg := describe(&pb.IntentGraphResponse{ResponseType: pb.IntentGraphMode_KNOWLEDGE_GRAPH, SpokenText: "Paris", QueryText: "capital of france"})
	if len(kg) != 2 || kg[0] != `kg "Paris"` || kg[1] != `query "capital of france"` {
		t.Fatalf("kg lines: %q", kg)
	}
}
