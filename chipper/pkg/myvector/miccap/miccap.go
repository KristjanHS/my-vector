// Package miccap saves each voice request's raw mic audio + its transcript for offline STT scoring.
// On only while the capture dir exists: $MYVECTOR_MIC_DIR, else robot/work/et-stt/mic (repo root or chipper/ cwd).
package miccap

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/digital-dream-labs/api/go/chipperpb"
)

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }

// Dir returns the capture dir, or "" when capture is off.
func Dir() string {
	if d := os.Getenv("MYVECTOR_MIC_DIR"); d != "" {
		if isDir(d) {
			return d
		}
		return ""
	}
	for _, d := range []string{"robot/work/et-stt/mic", "../robot/work/et-stt/mic"} {
		if isDir(d) {
			return d
		}
	}
	return ""
}

// Sink collects one request's audio bytes as the robot sent them (Ogg Opus or 16 kHz s16le PCM).
type Sink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *Sink) Write(p []byte) {
	s.mu.Lock()
	s.buf.Write(p)
	s.mu.Unlock()
}

type intentTee struct {
	pb.ChipperGrpc_StreamingIntentServer
	s *Sink
}

func (t intentTee) Recv() (*pb.StreamingIntentRequest, error) {
	r, err := t.ChipperGrpc_StreamingIntentServer.Recv()
	if err == nil {
		t.s.Write(r.InputAudio)
	}
	return r, err
}

type graphTee struct {
	pb.ChipperGrpc_StreamingIntentGraphServer
	s *Sink
}

func (t graphTee) Recv() (*pb.StreamingIntentGraphRequest, error) {
	r, err := t.ChipperGrpc_StreamingIntentGraphServer.Recv()
	if err == nil {
		t.s.Write(r.InputAudio)
	}
	return r, err
}

type kgTee struct {
	pb.ChipperGrpc_StreamingKnowledgeGraphServer
	s *Sink
}

func (t kgTee) Recv() (*pb.StreamingKnowledgeGraphRequest, error) {
	r, err := t.ChipperGrpc_StreamingKnowledgeGraphServer.Recv()
	if err == nil {
		t.s.Write(r.InputAudio)
	}
	return r, err
}

// Wrap returns stream with every Recv'd audio chunk teed into s; unknown types pass through.
func (s *Sink) Wrap(stream interface{}) interface{} {
	switch st := stream.(type) {
	case pb.ChipperGrpc_StreamingIntentServer:
		return intentTee{st, s}
	case pb.ChipperGrpc_StreamingIntentGraphServer:
		return graphTee{st, s}
	case pb.ChipperGrpc_StreamingKnowledgeGraphServer:
		return kgTee{st, s}
	}
	return stream
}

// Save writes <dir>/<stamp>.ogg (or .pcm) plus <stamp>.txt holding transcript; returns the audio path.
func (s *Sink) Save(dir, transcript string, now time.Time) (string, error) {
	s.mu.Lock()
	audio := append([]byte(nil), s.buf.Bytes()...)
	s.mu.Unlock()
	ext := ".pcm"
	if bytes.HasPrefix(audio, []byte("OggS")) {
		ext = ".ogg"
	}
	base := filepath.Join(dir, now.Format("20060102-150405.000"))
	if err := os.WriteFile(base+ext, audio, 0o644); err != nil {
		return "", err
	}
	return base + ext, os.WriteFile(base+".txt", []byte(transcript+"\n"), 0o644)
}
