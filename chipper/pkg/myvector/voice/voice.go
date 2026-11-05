// Package voice speaks text on Vector in a PC voice from the my-vector TTS service (tts/server.py, :8091),
// falling back to the robot's SDK voice (plan R9) when the service is down or fails before the first audio.
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
)

const (
	DefaultVoice = "cori"
	Fallback     = "fallback" // Say's voice name when SayText spoke
	rate         = 16000
	frame        = 1024                   // bytes per ExternalAudioStreamChunk (32 ms)
	volume       = 60                     // R6
	lead         = 300 * time.Millisecond // audio queued ahead of the speaker
)

var (
	// URL is the TTS service; MYVECTOR_TTS_URL overrides it.
	URL = envOr("MYVECTOR_TTS_URL", "http://127.0.0.1:8091")
	// FirstAudio bounds connect + headers; the service sends headers with the first sentence.
	FirstAudio = 3 * time.Second
	tail       = 300 * time.Millisecond // wait past the last queued frame
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Say speaks text in voice (at slow, 1 = normal) and returns the voice that spoke: voice, or Fallback with
// the reason. An error after the first PC audio is returned as is (no fallback: half the text already played).
func Say(ctx context.Context, v *vector.Vector, text, voice string, slow float64) (string, error) {
	body, err := fetch(ctx, text, voice, slow)
	var first []byte
	if err == nil {
		first, err = readFrame(body)
		if len(first) == 0 && err == io.EOF {
			err = errors.New("empty audio")
		}
	}
	if len(first) == 0 && err != nil {
		if body != nil {
			body.Close()
		}
		return fallback(ctx, v, text, err)
	}
	defer body.Close()
	return voice, stream(ctx, v, first, body)
}

func fetch(ctx context.Context, text, voice string, slow float64) (io.ReadCloser, error) {
	req, _ := json.Marshal(map[string]any{"text": text, "voice": voice, "slow": slow})
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, URL+"/tts", bytes.NewReader(req))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	c := http.Client{Transport: &http.Transport{ResponseHeaderTimeout: FirstAudio}}
	resp, err := c.Do(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		resp.Body.Close()
		return nil, fmt.Errorf("tts %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	return resp.Body, nil
}

// readFrame reads up to one frame; io.EOF only at a clean end with nothing read. Not io.ReadFull: its
// ErrUnexpectedEOF would hide a body cut mid-stream (net/http's own ErrUnexpectedEOF) as a short last frame.
func readFrame(r io.Reader) ([]byte, error) {
	b := make([]byte, frame)
	n := 0
	for n < frame {
		m, err := r.Read(b[n:])
		n += m
		if err == io.EOF && n > 0 {
			return b[:n], nil
		}
		if err != nil {
			return b[:n], err
		}
	}
	return b, nil
}

func fallback(ctx context.Context, v *vector.Vector, text string, why error) (string, error) {
	resp, err := v.Conn.SayText(ctx, &vectorpb.SayTextRequest{Text: text, UseVectorVoice: false, DurationScalar: 1.2}) // R9
	if err != nil {
		return Fallback, fmt.Errorf("%v; fallback SayText: %w", why, err)
	}
	if st := resp.GetState(); st != vectorpb.SayTextResponse_FINISHED {
		return Fallback, fmt.Errorf("%v; fallback SayText ended %s", why, st)
	}
	return Fallback + ": " + why.Error(), nil
}

// stream plays first + the rest of body through K2's audio stream, paced to real time with lead queued ahead,
// and returns once the audio has played out (or the robot reports completion or failure).
func stream(ctx context.Context, v *vector.Vector, first []byte, body io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c, err := v.Conn.ExternalAudioStreamPlayback(ctx)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { // the robot's verdict, if it sends one
		for {
			r, err := c.Recv()
			if err != nil {
				return
			}
			switch {
			case r.GetAudioStreamPlaybackComplete() != nil:
				done <- nil
				return
			case r.GetAudioStreamBufferOverrun() != nil:
				done <- errors.New("robot audio buffer overrun")
				return
			case r.GetAudioStreamPlaybackFailyer() != nil:
				done <- errors.New("robot audio playback failed")
				return
			}
		}
	}()
	err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
		AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: rate, AudioVolume: volume}}})
	end := time.Now() // when the audio queued so far finishes playing
	for b := first; err == nil && len(b) > 0; {
		if now := time.Now(); end.Before(now) {
			end = now
		}
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
			AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{AudioChunkSizeBytes: uint32(len(b)), AudioChunkSamples: b}}})
		end = end.Add(time.Duration(len(b)) * time.Second / (2 * rate))
		if err == nil {
			sleep(ctx, time.Until(end)-lead)
			if b, err = readFrame(body); err == io.EOF {
				err = nil
			}
		}
	}
	if err == nil {
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
			AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}})
	}
	if err != nil {
		return err
	}
	select {
	case err = <-done:
		return err
	case <-time.After(time.Until(end) + tail):
		return ctx.Err()
	}
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
