// Command voiceprobe plays listen-test speech on Vector beside the running wire-pod.
//
//	go run ./pkg/myvector/voiceprobe say  -dur 0.95 -vv=true "text"   # robot TTS (SayText)
//	go run ./pkg/myvector/voiceprobe play -vol 100 file.pcm             # 16 kHz s16le mono via K2's stream
//	go run ./pkg/myvector/voiceprobe cancel                             # silent: free a stream left open
//	go run ./pkg/myvector/voiceprobe overlap                            # silent: repro of two overlapping streams
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

// overlap is the silent repro of a Prepare arriving while another stream plays: stream A streams zeros, stream B
// prepares mid-way, then A finishes. It prints what each stream hears back. On stock vic-anim B's refused Prepare
// fails both (responses are broadcast), wipes A's state without ending its playback, and A's Complete is refused:
// A's playback starves forever.
func overlap(v *vector.Vector) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	open := func(name string) (vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, error) {
		c, err := v.Conn.ExternalAudioStreamPlayback(ctx)
		if err != nil {
			return nil, err
		}
		go func() {
			for {
				r, err := c.Recv()
				if err != nil {
					return
				}
				fmt.Printf("%s %s heard %T\n", time.Now().Format("15:04:05.000"), name, r.GetAudioResponseType())
			}
		}()
		return c, c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
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
	a, err := open("A")
	if err == nil {
		err = chunks(a, 40) // 1.3 s: posted after 0.2 s
	}
	if err != nil {
		return err
	}
	fmt.Println(time.Now().Format("15:04:05.000"), "B prepares")
	if _, err = open("B"); err != nil {
		return err
	}
	if err = chunks(a, 20); err != nil {
		return err
	}
	fmt.Println(time.Now().Format("15:04:05.000"), "A completes")
	if err = a.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
		AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}}); err != nil {
		return err
	}
	time.Sleep(3 * time.Second)
	return nil
}

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "voiceprobe:", err)
		os.Exit(1)
	}
}

// run refuses with the QUIET line before connecting while the robot sound lock holds (G5); say and play both make sound.
// cancel is silent, so it skips the lock and behavior control.
func run(args []string) error {
	if len(args) == 2 && (args[1] == "cancel" || args[1] == "overlap") {
		v, err := connect(botinfo.Path())
		if err != nil {
			return err
		}
		if args[1] == "overlap" {
			return overlap(v)
		}
		return cancelStream(v)
	}
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: voiceprobe say|play [flags] <text|file.pcm> | voiceprobe cancel")
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
