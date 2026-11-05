package miccap

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
)

type fakeIntent struct {
	pb.ChipperGrpc_StreamingIntentServer
	chunks [][]byte
}

func (f *fakeIntent) Recv() (*pb.StreamingIntentRequest, error) {
	if len(f.chunks) == 0 {
		return nil, io.EOF
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return &pb.StreamingIntentRequest{InputAudio: c}, nil
}

func TestWrapTeesAndKeepsStreamType(t *testing.T) {
	var s Sink
	s.Write([]byte("OggS"))
	w := s.Wrap(&fakeIntent{chunks: [][]byte{[]byte("-a"), []byte("-b")}})
	st, ok := w.(pb.ChipperGrpc_StreamingIntentServer)
	if !ok {
		t.Fatalf("wrapped stream lost its type: %T", w)
	}
	for {
		if _, err := st.Recv(); err != nil {
			break
		}
	}
	dir := t.TempDir()
	p, err := s.Save(dir, "mis kell on", time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(p) != ".ogg" {
		t.Errorf("ext = %s, want .ogg", p)
	}
	if b, _ := os.ReadFile(p); string(b) != "OggS-a-b" {
		t.Errorf("audio = %q, want %q", b, "OggS-a-b")
	}
	if b, _ := os.ReadFile(p[:len(p)-4] + ".txt"); string(b) != "mis kell on\n" {
		t.Errorf("transcript = %q", b)
	}
}

func TestSavePCMWhenNotOgg(t *testing.T) {
	var s Sink
	s.Write([]byte{1, 2})
	p, err := s.Save(t.TempDir(), "", time.Now())
	if err != nil || filepath.Ext(p) != ".pcm" {
		t.Errorf("path %s err %v, want .pcm", p, err)
	}
}

func TestDirOffUnlessExists(t *testing.T) {
	d := t.TempDir()
	t.Setenv("MYVECTOR_MIC_DIR", d)
	if Dir() != d {
		t.Errorf("Dir() = %q, want %q", Dir(), d)
	}
	t.Setenv("MYVECTOR_MIC_DIR", filepath.Join(d, "missing"))
	if Dir() != "" {
		t.Errorf("Dir() = %q for a missing dir, want off", Dir())
	}
}
