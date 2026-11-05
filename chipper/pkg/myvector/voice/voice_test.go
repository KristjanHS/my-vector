package voice

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc"
)

type robot struct {
	vectorpb.ExternalInterfaceClient // nil: any other method panics
	said                             []*vectorpb.SayTextRequest
	sent                             []*vectorpb.ExternalAudioStreamRequest
}

func (r *robot) SayText(_ context.Context, in *vectorpb.SayTextRequest, _ ...grpc.CallOption) (*vectorpb.SayTextResponse, error) {
	r.said = append(r.said, in)
	return &vectorpb.SayTextResponse{State: vectorpb.SayTextResponse_FINISHED}, nil
}

func (r *robot) ExternalAudioStreamPlayback(ctx context.Context, _ ...grpc.CallOption) (vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, error) {
	return &audio{r: r, ctx: ctx}, nil
}

type audio struct {
	grpc.ClientStream
	r   *robot
	ctx context.Context
}

func (a *audio) Send(m *vectorpb.ExternalAudioStreamRequest) error {
	a.r.sent = append(a.r.sent, m)
	return nil
}

func (a *audio) Recv() (*vectorpb.ExternalAudioStreamResponse, error) {
	<-a.ctx.Done()
	return nil, a.ctx.Err()
}

func service(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	old := URL
	URL = s.URL
	t.Cleanup(func() { URL = old })
}

func say(t *testing.T) (*robot, string, error) {
	t.Helper()
	r := &robot{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := Say(ctx, &vector.Vector{Conn: r}, "smoke test", "cori", 1)
	return r, got, err
}

func TestSayStreamsServiceAudio(t *testing.T) {
	var req map[string]any
	service(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts" {
			http.NotFound(w, r)
			return
		}
		json.NewDecoder(r.Body).Decode(&req)
		w.Write(make([]byte, 1500)) // two "sentences", flushed apart
		w.(http.Flusher).Flush()
		w.Write(make([]byte, 1500))
	})
	r, got, err := say(t)
	if err != nil || got != "cori" {
		t.Fatalf("Say = %q, %v; want cori, nil", got, err)
	}
	if req["text"] != "smoke test" || req["voice"] != "cori" || req["slow"] != 1.0 {
		t.Errorf("service request = %v", req)
	}
	if len(r.said) != 0 {
		t.Errorf("SayText called %d times with the service up", len(r.said))
	}
	p := r.sent[0].GetAudioStreamPrepare()
	if p == nil || p.AudioFrameRate != 16000 || p.AudioVolume != volume {
		t.Fatalf("first message = %v, want prepare 16000 Hz vol %d", r.sent[0], volume)
	}
	if r.sent[len(r.sent)-1].GetAudioStreamComplete() == nil {
		t.Errorf("last message = %v, want complete", r.sent[len(r.sent)-1])
	}
	total := 0
	for _, m := range r.sent[1 : len(r.sent)-1] {
		c := m.GetAudioStreamChunk()
		if c == nil || len(c.AudioChunkSamples) > frame || int(c.AudioChunkSizeBytes) != len(c.AudioChunkSamples) {
			t.Fatalf("middle message = %v, want a chunk of at most %d bytes", m, frame)
		}
		total += len(c.AudioChunkSamples)
	}
	if total != 3000 {
		t.Errorf("streamed %d bytes, want 3000", total)
	}
}

func TestSayFallsBack(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"500":   func(w http.ResponseWriter, r *http.Request) { http.Error(w, "engine down", 500) },
		"empty": func(w http.ResponseWriter, r *http.Request) {},
		"slow": func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond)
			w.Write(make([]byte, 100))
		},
	} {
		t.Run(name, func(t *testing.T) {
			service(t, h)
			old := FirstAudio
			FirstAudio = 100 * time.Millisecond
			t.Cleanup(func() { FirstAudio = old })
			r, got, err := say(t)
			if err != nil || !strings.HasPrefix(got, Fallback+": ") {
				t.Fatalf("Say = %q, %v; want %q + reason", got, err, Fallback)
			}
			if len(r.said) != 1 || r.said[0].UseVectorVoice || r.said[0].DurationScalar != 1.2 || r.said[0].Text != "smoke test" {
				t.Errorf("SayText = %v, want R9 (generic voice, 1.2)", r.said)
			}
			if len(r.sent) != 0 {
				t.Errorf("audio stream used on fallback: %d messages", len(r.sent))
			}
		})
	}
}

func TestSayFallsBackServiceDown(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := URL
	URL = "http://" + l.Addr().String()
	l.Close() // nothing listens: connection refused
	t.Cleanup(func() { URL = old })
	r, got, err := say(t)
	if err != nil || !strings.HasPrefix(got, Fallback+": ") || len(r.said) != 1 {
		t.Fatalf("Say = %q, %v, SayText %d; want fallback, one SayText", got, err, len(r.said))
	}
}

func TestSayMidStreamErrorNoFallback(t *testing.T) {
	service(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048)) // chunked, as tts/server.py streams

		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // cut the body mid-stream
	})
	r, got, err := say(t)
	if err == nil || got != "cori" || len(r.said) != 0 {
		t.Fatalf("Say = %q, %v, SayText %d; want cori + error, no SayText", got, err, len(r.said))
	}
}
