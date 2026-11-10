package wirepod_ttr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/lang"
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
	var asked []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Voice string }
		json.NewDecoder(r.Body).Decode(&req)
		asked = append(asked, req.Voice)
		http.Error(w, "no", http.StatusInternalServerError) // Speak falls back to SayText
	}))
	t.Cleanup(s.Close)
	oldURL := voice.URL
	voice.URL = s.URL
	t.Cleanup(func() { voice.URL = oldURL })
	if err := voice.Save(voice.Settings{T1: voice.Stock, T2: voice.Stock, T1ET: "mari", T2ET: "mari", Slow: 1.4}); err != nil {
		t.Fatal(err)
	}
	lang.UseForTest(t, lang.English)
	r := &sayRobot{}
	if err := DoSayText("hello", &vector.Vector{Conn: r}); err != nil {
		t.Fatal(err)
	}
	if len(r.said) != 1 || !r.said[0].UseVectorVoice || r.said[0].DurationScalar != float32(1.4) || r.said[0].Text != "hello" || len(asked) != 0 {
		t.Errorf("SayText = %v, service asked %v; want the saved stock voice at slow 1.4", r.said, asked)
	}
	lang.UseForTest(t, lang.Estonian)
	DoSayText("tere", &vector.Vector{Conn: &sayRobot{}})
	if len(asked) != 1 || asked[0] != "mari" {
		t.Errorf("et-EE: service asked %v, want the saved T1 mari", asked)
	}
}

type ctxSayRobot struct {
	vectorpb.ExternalInterfaceClient
	errs []error
}

func (r *ctxSayRobot) SayText(ctx context.Context, _ *vectorpb.SayTextRequest, _ ...grpc.CallOption) (*vectorpb.SayTextResponse, error) {
	r.errs = append(r.errs, ctx.Err())
	return nil, ctx.Err()
}

func TestForkSayTextCutByInterrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "myvectorConfig.json")
	old := voice.SettingsPath
	voice.SettingsPath = func() string { return path }
	t.Cleanup(func() { voice.SettingsPath = old })
	if err := voice.Save(voice.Settings{T1: voice.Stock, T2: voice.Stock, T1ET: "mari", T2ET: "mari", Slow: 1}); err != nil {
		t.Fatal(err)
	}
	lang.UseForTest(t, lang.English)
	r := &ctxSayRobot{}
	robot := &vector.Vector{Conn: r}
	stop := make(chan bool, 1)
	_, istop, end := forkInterruptBot(robot, stop)
	defer end()
	istop <- true
	<-stop
	forkSayText("hello", robot)
	if len(r.errs) != 1 || !errors.Is(r.errs[0], context.Canceled) {
		t.Fatalf("SayText ctx errs = %v, want [context.Canceled]", r.errs)
	}
}
