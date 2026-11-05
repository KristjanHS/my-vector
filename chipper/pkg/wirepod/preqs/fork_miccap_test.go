package processreqs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
	sr "github.com/kercre123/wire-pod/chipper/pkg/wirepod/speechrequest"
)

type forkFakeIntent struct {
	pb.ChipperGrpc_StreamingIntentServer
	chunks [][]byte
}

func (f *forkFakeIntent) Recv() (*pb.StreamingIntentRequest, error) {
	if len(f.chunks) == 0 {
		return nil, io.EOF
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return &pb.StreamingIntentRequest{InputAudio: c}, nil
}

// opusReq mirrors ReqToSpeechRequest's opus branch: FirstReq decoded+filtered, MicData the raw first page.
func opusReq() sr.SpeechRequest {
	return sr.SpeechRequest{
		FirstReq: []byte("decoded"),
		MicData:  []byte("OggS-raw"),
		IsOpus:   true,
		Stream:   &forkFakeIntent{chunks: [][]byte{[]byte("-a"), []byte("-b")}},
	}
}

func readAll(req sr.SpeechRequest) (string, error) {
	st := req.Stream.(pb.ChipperGrpc_StreamingIntentServer)
	for {
		if _, err := st.Recv(); err != nil {
			return "tekst", errors.New("stt err")
		}
	}
}

func TestForkMicCaptureSavesRawOpus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYVECTOR_MIC_DIR", dir)
	text, err := forkMicCapture(readAll)(opusReq())
	if text != "tekst" || err == nil || err.Error() != "stt err" {
		t.Errorf("handler result changed: %q, %v", text, err)
	}
	got, _ := filepath.Glob(filepath.Join(dir, "*.ogg"))
	if len(got) != 1 {
		t.Fatalf("want 1 .ogg capture, got %v", got)
	}
	if b, _ := os.ReadFile(got[0]); string(b) != "OggS-raw-a-b" {
		t.Errorf("capture = %q, want raw first page + chunks %q", b, "OggS-raw-a-b")
	}
}

func TestForkMicCaptureOffAndNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYVECTOR_MIC_DIR", filepath.Join(dir, "missing"))
	if text, _ := forkMicCapture(readAll)(opusReq()); text != "tekst" {
		t.Errorf("off path changed the transcript: %q", text)
	}
	if got, _ := filepath.Glob(filepath.Join(dir, "*", "*")); len(got) != 0 {
		t.Errorf("capture off but files written: %v", got)
	}
	if forkMicCapture(nil) != nil {
		t.Error("nil handler must stay nil (sti path)")
	}
}
