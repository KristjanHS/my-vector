// Command voiceprobe plays listen-test speech on Vector beside the running wire-pod.
//
//	go run ./pkg/myvector/voiceprobe say  -dur 0.95 -vv=true "text"   # robot TTS (SayText)
//	go run ./pkg/myvector/voiceprobe play -vol 100 file.pcm             # 16 kHz s16le mono via K2's stream
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
)

// botInfo is wire-pod's jdocs/botSdkInfo.json (vars.RobotInfoStore); the GUID never leaves this process.
type botInfo struct {
	Robots []struct {
		Esn       string `json:"esn"`
		IPAddress string `json:"ip_address"`
		GUID      string `json:"guid"`
	} `json:"robots"`
}

func connect(path string) (*vector.Vector, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bi botInfo
	if err := json.Unmarshal(raw, &bi); err != nil {
		return nil, err
	}
	if len(bi.Robots) == 0 {
		return nil, fmt.Errorf("no robots in %s", path)
	}
	r := bi.Robots[0]
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

func main() {
	info := os.Getenv("BOTINFO")
	if info == "" {
		info = os.ExpandEnv("$HOME/projects/my-vector/chipper/jdocs/botSdkInfo.json")
	}
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: voiceprobe say|play [flags] <text|file.pcm>")
		os.Exit(2)
	}
	if c := os.Args[1]; c != "say" && c != "play" {
		fmt.Fprintf(os.Stderr, "voiceprobe: unknown command %q\n", c)
		os.Exit(2)
	}
	v, err := connect(info)
	var release func()
	if err == nil {
		release, err = control(v)
	}
	if err == nil {
		defer release()
		switch os.Args[1] {
		case "say":
			err = say(v, os.Args[2:])
		case "play":
			err = play(v, os.Args[2:])
		default:
			err = fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "voiceprobe:", err)
		os.Exit(1)
	}
}
