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

type fakeGraph struct {
	pb.ChipperGrpc_StreamingIntentGraphServer
	chunks [][]byte
}

func (f *fakeGraph) Recv() (*pb.StreamingIntentGraphRequest, error) {
	if len(f.chunks) == 0 {
		return nil, io.EOF
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return &pb.StreamingIntentGraphRequest{InputAudio: c}, nil
}

type fakeKG struct {
	pb.ChipperGrpc_StreamingKnowledgeGraphServer
	chunks [][]byte
}

func (f *fakeKG) Recv() (*pb.StreamingKnowledgeGraphRequest, error) {
	if len(f.chunks) == 0 {
		return nil, io.EOF
	}
	c := f.chunks[0]
	f.chunks = f.chunks[1:]
	return &pb.StreamingKnowledgeGraphRequest{InputAudio: c}, nil
}

func chunks() [][]byte { return [][]byte{[]byte("-a"), []byte("-b")} }

// drain reads the wrapped stream through the interface the STT engines assert on.
func drain(t *testing.T, w interface{}) {
	t.Helper()
	var recv func() error
	switch st := w.(type) {
	case pb.ChipperGrpc_StreamingIntentServer:
		recv = func() error { _, err := st.Recv(); return err }
	case pb.ChipperGrpc_StreamingIntentGraphServer:
		recv = func() error { _, err := st.Recv(); return err }
	case pb.ChipperGrpc_StreamingKnowledgeGraphServer:
		recv = func() error { _, err := st.Recv(); return err }
	default:
		t.Fatalf("wrapped stream lost its type: %T", w)
	}
	for recv() == nil {
	}
}

func TestWrapTeesAllThreeStreamKinds(t *testing.T) {
	for name, stream := range map[string]interface{}{
		"intent": &fakeIntent{chunks: chunks()},
		"graph":  &fakeGraph{chunks: chunks()},
		"kg":     &fakeKG{chunks: chunks()},
	} {
		t.Run(name, func(t *testing.T) {
			var s Sink
			s.Write([]byte("raw"))
			drain(t, s.Wrap(stream))
			p, err := s.Save(t.TempDir(), "mis kell on", true, time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Ext(p) != ".ogg" {
				t.Errorf("ext = %s, want .ogg", p)
			}
			if b, _ := os.ReadFile(p); string(b) != "raw-a-b" {
				t.Errorf("audio = %q, want %q", b, "raw-a-b")
			}
			if b, _ := os.ReadFile(p[:len(p)-4] + ".txt"); string(b) != "mis kell on\n" {
				t.Errorf("transcript = %q", b)
			}
		})
	}
}

func TestSavePCMWhenNotOpus(t *testing.T) {
	var s Sink
	s.Write([]byte("OggS"))
	p, err := s.Save(t.TempDir(), "", false, time.Now())
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
