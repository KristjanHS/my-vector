// Command fakebot streams an utterance into wire-pod's chipper gRPC as WireOS vic-cloud does
// (StreamingIntentGraph) and prints the replies. Exit 0 = a reply (matching -want), 1 = none or
// a -want mismatch, 2 = setup or transport error.
//
//	go run ./pkg/myvector/fakebot -pcm file.pcm [-want intent_clock_time]   # 16 kHz s16le mono
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	"github.com/google/uuid"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/botinfo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const chunk = 3200 // 100 ms of 16 kHz s16le

// pace is the gap between chunks (real time); tests set it to 0.
var pace = 100 * time.Millisecond

// frame prepends 100 ms of zeros (the server sniffs Opus by InputAudio[0]==0x4f) and appends
// 1 s of zeros so end-of-speech fires before CloseSend.
func frame(pcm []byte) []byte {
	out := make([]byte, chunk, chunk+len(pcm)+10*chunk)
	out = append(out, pcm...)
	return append(out, make([]byte, 10*chunk)...)
}

// run streams pcm as one voice-command utterance and returns every reply until the server closes.
func run(ctx context.Context, client pb.ChipperGrpcClient, esn string, pcm []byte) ([]*pb.IntentGraphResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.StreamingIntentGraph(ctx)
	if err != nil {
		return nil, err
	}
	audio, session := frame(pcm), uuid.New().String()[:16]
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := 0; i < len(audio); i += chunk {
			j := i + chunk
			if j > len(audio) {
				j = len(audio)
			}
			req := &pb.StreamingIntentGraphRequest{InputAudio: audio[i:j]}
			if i == 0 {
				req.DeviceId, req.Session = esn, session
				req.LanguageCode, req.AudioEncoding, req.Mode = pb.LanguageCode_ENGLISH_US, pb.AudioEncoding_LINEAR_PCM, pb.RobotMode_VOICE_COMMAND
			}
			// A Send error ends the stream; Recv reports its status.
			if stream.Send(req) != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(pace):
			}
		}
		stream.CloseSend()
	}()
	defer func() { cancel(); <-sent }()
	var out []*pb.IntentGraphResponse
	for {
		r, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
}

func describe(r *pb.IntentGraphResponse) []string {
	var lines []string
	if r.ResponseType == pb.IntentGraphMode_KNOWLEDGE_GRAPH {
		lines = append(lines, fmt.Sprintf("kg %q", r.SpokenText))
	} else {
		lines = append(lines, fmt.Sprintf("intent %s %v", r.GetIntentResult().GetAction(), r.GetIntentResult().GetParameters()))
	}
	q := r.QueryText
	if q == "" {
		q = r.GetIntentResult().GetQueryText()
	}
	if q != "" {
		lines = append(lines, fmt.Sprintf("query %q", q))
	}
	return lines
}

// verdict is the exit code for replies rs: 1 without a reply or without a want-matching intent.
func verdict(rs []*pb.IntentGraphResponse, want string) int {
	if len(rs) == 0 {
		return 1
	}
	if want == "" {
		return 0
	}
	for _, r := range rs {
		if r.GetIntentResult().GetAction() == want {
			return 0
		}
	}
	return 1
}

func setup(err error) {
	fmt.Fprintln(os.Stderr, "fakebot:", err)
	os.Exit(2)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:443", "chipper gRPC host:port")
	esn := flag.String("esn", "", "DeviceId (default: robots[0].esn of $BOTINFO / jdocs/botSdkInfo.json)")
	pcmPath := flag.String("pcm", "", "16 kHz s16le mono file to stream")
	want := flag.String("want", "", "exit 1 unless a reply carries this intent action")
	timeout := flag.Duration("timeout", 60*time.Second, "whole-run deadline")
	flag.Parse()
	if *pcmPath == "" {
		setup(errors.New("need -pcm"))
	}
	pcm, err := os.ReadFile(*pcmPath)
	if err != nil {
		setup(err)
	}
	if *esn == "" {
		r, err := botinfo.Load(botinfo.Path())
		if err != nil {
			setup(err)
		}
		*esn = r.Esn
	}
	// wire-pod serves a self-signed cert and checks no client credentials.
	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	if err != nil {
		setup(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	rs, err := run(ctx, pb.NewChipperGrpcClient(conn), *esn, pcm)
	cancel()
	conn.Close()
	for _, r := range rs {
		for _, l := range describe(r) {
			fmt.Println(l)
		}
	}
	if err != nil {
		setup(err)
	}
	os.Exit(verdict(rs, *want))
}
