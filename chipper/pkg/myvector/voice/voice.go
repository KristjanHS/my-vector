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
	"sync"
	"sync/atomic"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultVoice = "af_bella"
	Fallback     = "fallback" // Say's voice name when SayText spoke
	rate         = 16000
	frame        = 1024                   // bytes per ExternalAudioStreamChunk (32 ms)
	volume       = 100                    // R23: the stream path's max; 60 (R6) was picked at master 2
	boostDB      = 2.0                    // R23: on top of the service's -14 LUFS base; ~3% of speech already limited >3 dB, no headroom left
	lead         = 300 * time.Millisecond // audio queued ahead of the speaker
	grace        = 2 * time.Second        // the stream outlives the caller ctx by this, for its Cancel
	refusedRetry = 200 * time.Millisecond // wait before retrying a stream the gateway refused
)

var (
	// URL is the TTS service; MYVECTOR_TTS_URL overrides it.
	URL = envOr("MYVECTOR_TTS_URL", "http://127.0.0.1:8091")
	// FirstAudio bounds dial + request + headers; the service sends headers with the first sentence.
	FirstAudio = 3 * time.Second
	// Stall bounds each later read: one sentence's synthesis (Kokoro CPU fallback ~4 s).
	Stall  = 10 * time.Second
	tail   = time.Second                                                                 // wait past the last queued frame for the robot's PlaybackComplete
	client = &http.Client{Transport: &http.Transport{IdleConnTimeout: 20 * time.Second}} // server idles out at 30 s
)

type firstAudioKey struct{}

// WithFirstAudio is ctx with its own FirstAudio bound, for a caller that would rather wait out a cold model
// load than fall back (the smoke test).
func WithFirstAudio(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, firstAudioKey{}, d)
}

func firstAudio(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(firstAudioKey{}).(time.Duration); ok {
		return d
	}
	return FirstAudio
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Say speaks text in voice (at slow, 1 = normal) and returns the voice that spoke: voice, or Fallback with
// the reason. An error after the first PC audio is returned as is (no fallback: half the text already played),
// except a stream the gateway refused (ResourceExhausted: one per robot, nothing played): retried once, then fallback.
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
	var played bytes.Buffer // what the first try read from body, replayed on the retry
	err = stream(ctx, v, first, io.TeeReader(body, &played))
	if status.Code(err) == codes.ResourceExhausted {
		sleep(ctx, refusedRetry)
		if err = ctx.Err(); err == nil {
			err = stream(ctx, v, first, io.MultiReader(&played, body))
		}
		if status.Code(err) == codes.ResourceExhausted {
			return fallback(ctx, v, text, err)
		}
	}
	return voice, err
}

// Speak says text in s.T1 at s.Slow and returns the voice that spoke, as Say does; Stock is the robot's Acapela voice (plan R12; no robot-voice effect since patch 0010).
func Speak(ctx context.Context, v *vector.Vector, text string, s Settings) (string, error) {
	if s.T1 != Stock {
		return Say(ctx, v, text, s.T1, s.Slow)
	}
	resp, err := v.Conn.SayText(ctx, &vectorpb.SayTextRequest{Text: text, UseVectorVoice: true, DurationScalar: float32(s.Slow)})
	if err == nil && resp.GetState() != vectorpb.SayTextResponse_FINISHED {
		err = fmt.Errorf("SayText ended %s", resp.GetState())
	}
	return Stock, err
}

// ControlWait bounds the wait for the robot to grant behavior control.
var ControlWait = 10 * time.Second

// Control takes SDK behavior control and returns its release. SayText (Stock and the R9 fallback) fails
// "Failed to say text" without it; LLM answers already speak inside kgsim's forkBControl (ttr fork_answer.go), so only callers outside it need this.
// The engine has one control slot with no owner check (sdkComponent.cpp): a second holder's request is granted at once
// and either holder's release ends control for both, so a demo overlapping an LLM answer can fail the other's SayText.
func Control(ctx context.Context, v *vector.Vector) (func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(ControlWait, cancel)
	r, err := v.Conn.BehaviorControl(ctx)
	if err == nil {
		err = r.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
			ControlRequest: &vectorpb.ControlRequest{Priority: vectorpb.ControlRequest_OVERRIDE_BEHAVIORS}}})
	}
	for err == nil {
		var resp *vectorpb.BehaviorControlResponse
		if resp, err = r.Recv(); err == nil && resp.GetControlGrantedResponse() != nil {
			if !timer.Stop() {
				break // ControlWait fired as the grant arrived: the stream is already cancelled
			}
			return func() {
				r.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRelease{
					ControlRelease: &vectorpb.ControlRelease{}}})
				cancel()
			}, nil
		}
	}
	if !timer.Stop() && ctx.Err() != nil {
		err = fmt.Errorf("behavior control not granted within %s (robot unreachable or busy)", ControlWait)
	}
	cancel()
	return nil, err
}

func fetch(ctx context.Context, text, voice string, slow float64) (io.ReadCloser, error) {
	req, _ := json.Marshal(map[string]any{"text": text, "voice": voice, "slow": slow, "boost_db": boostDB})
	hctx, cancel := context.WithCancel(ctx)
	r, err := http.NewRequestWithContext(hctx, http.MethodPost, URL+"/tts", bytes.NewReader(req))
	if err != nil {
		cancel()
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	wait := firstAudio(ctx)
	timer := time.AfterFunc(wait, cancel)
	resp, err := client.Do(r)
	if !timer.Stop() && ctx.Err() == nil {
		err = fmt.Errorf("no audio from %s within %s", URL, wait)
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("tts %d: %s", resp.StatusCode, bytes.Join(bytes.Fields(msg), []byte(" ")))
	}
	b := &body{ReadCloser: resp.Body, cancel: cancel}
	b.watch = time.AfterFunc(time.Hour, func() { b.stalled.Store(true); cancel() })
	b.watch.Stop()
	return b, nil
}

// body ends the request context with the body, and cancels it when one Read waits past Stall.
type body struct {
	io.ReadCloser
	cancel  context.CancelFunc
	watch   *time.Timer
	stalled atomic.Bool
}

func (b *body) Read(p []byte) (int, error) {
	b.watch.Reset(Stall)
	n, err := b.ReadCloser.Read(p)
	b.watch.Stop()
	if err != nil && b.stalled.Load() {
		err = fmt.Errorf("tts stalled: no audio for %s", Stall)
	}
	return n, err
}

func (b *body) Close() error { b.watch.Stop(); defer b.cancel(); return b.ReadCloser.Close() }

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
	free, err := turn(ctx, v.Cfg.SerialNo)
	if err != nil {
		return err
	}
	defer free()
	sctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer context.AfterFunc(ctx, func() { time.AfterFunc(grace, cancel) })()
	c, err := v.Conn.ExternalAudioStreamPlayback(sctx)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	var ours, verdict atomic.Bool // ours: our Complete went out; verdict: the robot ended the stream itself: no Cancel
	go func() {
		for {
			r, err := c.Recv()
			if err != nil {
				if _, ok := status.FromError(err); ok {
					done <- err // the gateway's refusal shows only here: Send gets io.EOF
				}
				return
			}
			var e error
			switch {
			case r.GetAudioStreamPlaybackComplete() != nil:
				if !ours.Load() {
					continue // an earlier playback's broadcast
				}
			case r.GetAudioStreamBufferOverrun() != nil:
				e = errors.New("robot audio buffer overrun")
			case r.GetAudioStreamPlaybackFailyer() != nil:
				e = errors.New("robot audio playback failed")
			default:
				continue
			}
			verdict.Store(true)
			done <- e
			return
		}
	}()
	err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
		AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: rate, AudioVolume: volume}}})
	completed := false
	if err == nil {
		defer func() { // a prepared stream left open starves vic-anim (sdk-audio-orphan.md (docs/plans/))
			if !completed && !verdict.Load() {
				c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamCancel{
					AudioStreamCancel: &vectorpb.ExternalAudioStreamCancel{}}})
			}
		}()
	}
	end := time.Now() // when the audio queued so far finishes playing
	for b := first; err == nil && len(b) > 0; {
		if now := time.Now(); end.Before(now) {
			end = now
		}
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
			AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{AudioChunkSizeBytes: uint32(len(b)), AudioChunkSamples: b}}})
		end = end.Add(time.Duration(len(b)) * time.Second / (2 * rate))
		select {
		case e := <-done: // overrun, failure or a closed stream; a PlaybackComplete counts only after our Complete
			return e // (vic-anim sends it only after Complete/Cancel/cleanup, so an earlier one is stale)
		default:
		}
		if err == nil {
			sleep(ctx, time.Until(end)-lead)
			if err = ctx.Err(); err == nil {
				if b, err = readFrame(body); err == io.EOF {
					err = nil
				}
			}
		}
	}
	if err == nil {
		ours.Store(true) // before Send: the robot's PlaybackComplete can beat its return
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
			AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}})
		completed = err == nil
	}
	if errors.Is(err, io.EOF) { // the stream is closed: its status reaches Recv only
		select {
		case e := <-done:
			if e != nil {
				err = e
			}
		case <-time.After(tail):
		}
	}
	if err != nil {
		return err
	}
	select {
	case err = <-done:
		return err
	case <-ctx.Done(): // interrupted while our audio still plays: stop the robot (sctx outlives ctx by grace)
		if !verdict.Load() {
			c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamCancel{
				AudioStreamCancel: &vectorpb.ExternalAudioStreamCancel{}}})
		}
		return ctx.Err()
	case <-time.After(time.Until(end) + tail):
		logger.Println("myvector voice: no robot verdict " + tail.String() + " past the audio's end; stream ended unconfirmed")
		return ctx.Err()
	}
}

var streams = struct {
	sync.Mutex
	slot map[string]chan struct{}
}{slot: map[string]chan struct{}{}}

// turn waits for serial's one audio stream slot: a Prepare onto a playing stream orphans it (sdk-audio-orphan.md (docs/plans/)).
func turn(ctx context.Context, serial string) (func(), error) {
	streams.Lock()
	c := streams.slot[serial]
	if c == nil {
		c = make(chan struct{}, 1)
		streams.slot[serial] = c
	}
	streams.Unlock()
	select {
	case c <- struct{}{}:
		return func() { <-c }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
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
