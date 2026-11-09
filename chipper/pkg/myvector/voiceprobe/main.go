// Command voiceprobe plays listen-test speech on Vector beside the running wire-pod.
//
//	go run ./pkg/myvector/voiceprobe say  -dur 0.95 -vv=true "text"   # robot TTS (SayText)
//	go run ./pkg/myvector/voiceprobe play -vol 100 file.pcm             # 16 kHz s16le mono via K2's stream
//	go run ./pkg/myvector/voiceprobe cancel                             # silent: free a stream left open
//	go run ./pkg/myvector/voiceprobe overlap                            # silent: overlapping + short streams
//	go run ./pkg/myvector/voiceprobe race                               # silent: a closed stream's Cancel must not cut the playing one
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/botinfo"
	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

func connect(path string) (*vector.Vector, error) {
	r, err := botinfo.Load(path)
	if err != nil {
		return nil, err
	}
	return vector.New(vector.WithSerialNo(r.Esn), vector.WithToken(r.GUID), vector.WithTarget(r.IPAddress+":443"))
}

// control takes SDK behavior control (SayText fails "Failed to say text" without it); the release closes the stream.
func control(v *vector.Vector) (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	r, err := v.Conn.BehaviorControl(ctx)
	if err == nil {
		err = r.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
			ControlRequest: &vectorpb.ControlRequest{Priority: vectorpb.ControlRequest_OVERRIDE_BEHAVIORS}}})
	}
	for err == nil {
		var resp *vectorpb.BehaviorControlResponse
		if resp, err = r.Recv(); err == nil && resp.GetControlGrantedResponse() != nil {
			return func() {
				r.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRelease{
					ControlRelease: &vectorpb.ControlRelease{}}})
				cancel()
			}, nil
		}
	}
	cancel()
	return nil, err
}

func say(v *vector.Vector, args []string) error {
	fs := flag.NewFlagSet("say", flag.ExitOnError)
	dur := fs.Float64("dur", 0.95, "DurationScalar (>1 = slower)")
	vv := fs.Bool("vv", true, "UseVectorVoice")
	fs.Parse(args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := v.Conn.SayText(ctx, &vectorpb.SayTextRequest{Text: fs.Arg(0), DurationScalar: float32(*dur), UseVectorVoice: *vv})
	if err != nil {
		return err
	}
	fmt.Println(resp.GetState())
	return nil
}

// play streams 1024-byte chunks (32 ms of audio) every 30 ms, then waits until the clip has played out.
func play(v *vector.Vector, args []string) error {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	vol := fs.Uint("vol", 100, "AudioVolume 0-100")
	fs.Parse(args)
	pcm, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	c, err := v.Conn.ExternalAudioStreamPlayback(context.Background())
	if err != nil {
		return err
	}
	start, end := time.Now(), time.Now().Add(time.Duration(len(pcm))*time.Second/32000+300*time.Millisecond)
	err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
		AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: 16000, AudioVolume: uint32(*vol)}}})
	for err == nil && len(pcm) > 0 {
		n := 1024
		if len(pcm) < n {
			n = len(pcm)
		}
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
			AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{AudioChunkSizeBytes: uint32(n), AudioChunkSamples: pcm[:n]}}})
		pcm = pcm[n:]
		time.Sleep(30 * time.Millisecond)
	}
	if err == nil {
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
			AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}})
	}
	if err != nil {
		return fmt.Errorf("stream after %s: %w", time.Since(start).Round(time.Millisecond), err)
	}
	time.Sleep(time.Until(end))
	return nil
}

// cancelStream sends Prepare then Cancel with no audio: silent. It frees vic-anim's one SDK audio slot when a client
// left it prepared (every later Prepare is refused "Already playing audio"). Before firmware patch 0019 it cannot
// end an orphaned playback (Cancel stopped nothing); restart anki-robot.target. Prepare's volume is the voice path's.
func cancelStream(v *vector.Vector) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := v.Conn.ExternalAudioStreamPlayback(ctx)
	if err == nil {
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
			AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: 16000, AudioVolume: 100}}})
	}
	if err == nil {
		err = c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamCancel{
			AudioStreamCancel: &vectorpb.ExternalAudioStreamCancel{}}})
	}
	time.Sleep(500 * time.Millisecond) // let the gateway forward it before the stream closes
	return err
}

// overlap (silent, zeros): A streams, B streams + completes over it, A completes; then singly C empty, D 96 ms (<0.2 s
// start), E closes unfinished, F empty. With 0019: B ends ResourceExhausted; A, C, D, F hear PlaybackComplete; E nothing.
func overlap(v *vector.Vector) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stamp := func() string { return time.Now().Format("15:04:05.000") }
	// open prepares a stream; heard ticks on its first response or its end.
	open := func(ctx context.Context, name string) (vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, <-chan struct{}, error) {
		c, err := v.Conn.ExternalAudioStreamPlayback(ctx)
		if err != nil {
			return nil, nil, err
		}
		heard := make(chan struct{}, 1)
		go func() {
			for {
				r, err := c.Recv()
				select {
				case heard <- struct{}{}:
				default:
				}
				if err != nil {
					fmt.Printf("%s %s ended: %v\n", stamp(), name, err)
					return
				}
				fmt.Printf("%s %s heard %T\n", stamp(), name, r.GetAudioResponseType())
			}
		}()
		return c, heard, c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
			AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: 16000, AudioVolume: 100}}})
	}
	zeros := make([]byte, 1024)
	chunks := func(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, n int) error {
		for i := 0; i < n; i++ {
			if err := c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
				AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{AudioChunkSizeBytes: 1024, AudioChunkSamples: zeros}}}); err != nil {
				return err
			}
			time.Sleep(32 * time.Millisecond)
		}
		return nil
	}
	complete := func(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient) error {
		return c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
			AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}})
	}
	pctx, pcancel := context.WithCancel(ctx)
	defer pcancel()
	a, _, err := open(pctx, "A")
	if err == nil {
		err = chunks(a, 40) // 1.3 s: posted after 0.2 s
	}
	if err != nil {
		return err
	}
	fmt.Println(stamp(), "B prepares, streams 0.6 s and completes while A streams")
	b, _, berr := open(pctx, "B")
	if b == nil {
		return berr
	}
	bDone := make(chan struct{})
	go func() { // B's send errors are expected (refused at the gateway): printed, not fatal
		defer close(bDone)
		err := berr
		if err == nil {
			err = chunks(b, 20)
		}
		if err == nil {
			err = complete(b)
		}
		if err != nil {
			fmt.Printf("%s B send: %v\n", stamp(), err)
		}
	}()
	err = chunks(a, 30)
	<-bDone
	if err != nil {
		return err
	}
	fmt.Println(stamp(), "A completes")
	if err = complete(a); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	pcancel()
	time.Sleep(300 * time.Millisecond) // the gateway frees its one stream slot
	for _, s := range []struct {
		name     string
		n        int
		complete bool
	}{{"C", 0, true}, {"D", 3, true}, {"E", 3, false}, {"F", 0, true}} {
		fmt.Printf("%s %s prepares, %d chunks, complete=%v\n", stamp(), s.name, s.n, s.complete)
		sctx, scancel := context.WithCancel(ctx)
		c, heard, err := open(sctx, s.name)
		if err == nil {
			err = chunks(c, s.n)
		}
		if err == nil && s.complete {
			err = complete(c)
		}
		if err == nil && s.complete {
			select {
			case <-heard:
			case <-time.After(2 * time.Second):
				fmt.Println(stamp(), s.name, "heard nothing in 2 s")
			}
		}
		scancel()
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		time.Sleep(300 * time.Millisecond) // E's close: the gateway's Cancel lands before F's Prepare
	}
	return nil
}

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "voiceprobe:", err)
		os.Exit(1)
	}
}

// run refuses with the QUIET line before connecting while the robot sound lock holds (G5); say and play both make sound.
// cancel, overlap and race are silent, so they skip the lock and behavior control.
func run(args []string) error {
	if len(args) == 2 && (args[1] == "cancel" || args[1] == "overlap" || args[1] == "race") {
		v, err := connect(botinfo.Path())
		if err != nil {
			return err
		}
		switch args[1] {
		case "overlap":
			return overlap(v)
		case "race":
			return race(v)
		}
		return cancelStream(v)
	}
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: voiceprobe say|play [flags] <text|file.pcm> | voiceprobe cancel|overlap|race")
		os.Exit(2)
	}
	if c := args[1]; c != "say" && c != "play" {
		fmt.Fprintf(os.Stderr, "voiceprobe: unknown command %q\n", c)
		os.Exit(2)
	}
	if line, locked := quiet.Line(time.Now()); locked {
		return errors.New(line)
	}
	v, err := connect(botinfo.Path())
	if err != nil {
		return err
	}
	release, err := control(v)
	if err != nil {
		return err
	}
	defer release()
	if args[1] == "say" {
		return say(v, args[2:])
	}
	return play(v, args[2:])
}
