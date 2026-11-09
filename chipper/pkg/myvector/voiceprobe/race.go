package main

import (
	"context"
	"fmt"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	raceChunks = 94 // A's audio: 94 × 32 ms ≈ 3 s
	tsLayout   = "15:04:05.000"
)

func ts() string { return time.Now().Format(tsLayout) }

type event struct {
	at  time.Time
	r   *vectorpb.ExternalAudioStreamResponse
	err error
}

// watch prints and forwards every response of c until its stream ends.
func watch(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, name string) <-chan event {
	ch := make(chan event, 16)
	go func() {
		defer close(ch)
		for {
			r, err := c.Recv()
			e := event{time.Now(), r, err}
			if err != nil {
				fmt.Printf("%s %s ended: %v\n", e.at.Format(tsLayout), name, err)
			} else {
				fmt.Printf("%s %s heard %T\n", e.at.Format(tsLayout), name, r.GetAudioResponseType())
			}
			select {
			case ch <- e:
			default:
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

func sendPrepare(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient) error {
	return c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamPrepare{
		AudioStreamPrepare: &vectorpb.ExternalAudioStreamPrepare{AudioFrameRate: 16000, AudioVolume: 100}}})
}

// sendZeros sends n silent 32 ms chunks, pace apart.
func sendZeros(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, n int, pace time.Duration) error {
	zeros := make([]byte, 1024)
	for i := 0; i < n; i++ {
		if err := c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamChunk{
			AudioStreamChunk: &vectorpb.ExternalAudioStreamChunk{AudioChunkSizeBytes: 1024, AudioChunkSamples: zeros}}}); err != nil {
			return err
		}
		time.Sleep(pace)
	}
	return nil
}

func sendComplete(c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient) error {
	return c.Send(&vectorpb.ExternalAudioStreamRequest{AudioRequestType: &vectorpb.ExternalAudioStreamRequest_AudioStreamComplete{
		AudioStreamComplete: &vectorpb.ExternalAudioStreamComplete{}}})
}

// race (silent, zeros): A queues 3 s + Complete and closes at once; B Prepares and closes at once; C retries
// ResourceExhausted until admitted, streams 0.1 s + Complete. Pass: C's PlaybackComplete ≥ 3 s after A's Complete.
func race(v *vector.Vector) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(name string) (vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, <-chan event, context.CancelFunc, error) {
		sctx, scancel := context.WithCancel(ctx)
		c, err := v.Conn.ExternalAudioStreamPlayback(sctx)
		if err != nil {
			scancel()
			return nil, nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		return c, watch(c, name), scancel, sendPrepare(c)
	}

	a, _, closeA, err := open("A")
	if err == nil {
		err = sendZeros(a, raceChunks, 0)
	}
	if err == nil {
		err = sendComplete(a)
	}
	aDone := time.Now()
	if closeA != nil {
		closeA()
	}
	if err != nil {
		return fmt.Errorf("A: %w", err)
	}
	fmt.Println(ts(), "A queued 3 s, completed and closed")

	_, _, closeB, err := open("B")
	if closeB != nil {
		closeB()
	}
	fmt.Println(ts(), "B prepared and closed:", err)

	for try := 1; ; try++ {
		c, ev, closeC, err := open("C")
		if c == nil {
			return err
		}
		if err == nil {
			err = sendZeros(c, 3, 32*time.Millisecond)
		}
		var sent time.Time
		if err == nil {
			sent = time.Now() // a PlaybackComplete before C's Complete is an earlier playback's broadcast
			err = sendComplete(c)
		}
		e, ok := verdictAfter(ctx, ev, sent)
		closeC()
		switch {
		case ok && status.Code(e.err) == codes.ResourceExhausted:
			time.Sleep(200 * time.Millisecond)
			continue
		case !ok:
			return fmt.Errorf("race: C (try %d) got no verdict (send err %v)", try, err)
		case e.r.GetAudioStreamPlaybackComplete() == nil:
			return fmt.Errorf("race: C (try %d) ended %v %T", try, e.err, e.r.GetAudioResponseType())
		}
		gap := e.at.Sub(aDone)
		if gap < raceChunks*32*time.Millisecond {
			return fmt.Errorf("race: FAIL C's PlaybackComplete %s after A's Complete, want ≥ 3 s (A's playback was cut)", gap.Round(time.Millisecond))
		}
		fmt.Printf("%s race: PASS C's PlaybackComplete %s after A's Complete\n", ts(), gap.Round(time.Millisecond))
		return nil
	}
}

// verdictAfter is C's first stream end or a PlaybackComplete/Failure/Overrun after sent (zero: none yet sent).
func verdictAfter(ctx context.Context, ev <-chan event, sent time.Time) (event, bool) {
	for {
		select {
		case e, more := <-ev:
			if !more {
				return event{}, false
			}
			if e.err != nil {
				return e, true
			}
			if e.r.GetAudioStreamPlaybackComplete() != nil && (sent.IsZero() || e.at.Before(sent)) {
				continue
			}
			switch {
			case e.r.GetAudioStreamPlaybackComplete() != nil, e.r.GetAudioStreamPlaybackFailyer() != nil, e.r.GetAudioStreamBufferOverrun() != nil:
				return e, true
			}
		case <-ctx.Done():
			return event{}, false
		}
	}
}
