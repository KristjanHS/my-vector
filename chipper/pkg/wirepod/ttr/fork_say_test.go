package wirepod_ttr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/voice"
	"google.golang.org/grpc"
)

type sayRobot struct {
	vectorpb.ExternalInterfaceClient // nil: any other method panics
	said                             []*vectorpb.SayTextRequest
}

func (r *sayRobot) SayText(_ context.Context, in *vectorpb.SayTextRequest, _ ...grpc.CallOption) (*vectorpb.SayTextResponse, error) {
	r.said = append(r.said, in)
	return &vectorpb.SayTextResponse{State: vectorpb.SayTextResponse_FINISHED}, nil
}

func TestForkDoSayTextUsesSavedVoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	old := voice.SettingsPath
	voice.SettingsPath = func() string { return path }
	t.Cleanup(func() { voice.SettingsPath = old })
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("service called for stock") }))
	t.Cleanup(s.Close)
	oldURL := voice.URL
	voice.URL = s.URL
	t.Cleanup(func() { voice.URL = oldURL })
	if err := voice.Save(voice.Settings{T1: voice.Stock, Slow: 1.4}); err != nil {
		t.Fatal(err)
	}
	r := &sayRobot{}
	if err := DoSayText("hello", &vector.Vector{Conn: r}); err != nil {
		t.Fatal(err)
	}
	if len(r.said) != 1 || !r.said[0].UseVectorVoice || r.said[0].DurationScalar != float32(1.4) || r.said[0].Text != "hello" {
		t.Errorf("SayText = %v, want the saved stock voice at slow 1.4", r.said)
	}
}
