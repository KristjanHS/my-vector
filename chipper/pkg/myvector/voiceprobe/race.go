package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fforchino/vector-go-sdk/pkg/vector"
	"github.com/fforchino/vector-go-sdk/pkg/vectorpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	raceChunks = 94 // A's audio: 94 × 32 ms ≈ 3 s
	raceTries  = 10 // B's admission attempts
	tsLayout   = "15:04:05.000"
)

func ts() string { return time.Now().Format(tsLayout) }

type event struct {
	at  time.Time
	r   *vectorpb.ExternalAudioStreamResponse
	err error
}

// watch prints and forwards every response of c until its stream ends or ctx is done.
func watch(ctx context.Context, c vectorpb.ExternalInterface_ExternalAudioStreamPlaybackClient, name string) <-chan event {
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
			case <-ctx.Done():
				return
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

// race (silent, zeros): A streams 3 s at 2× real time + Complete, closes; B Prepares (retried until admitted), held 100 ms, closes;
// C retries refusals, streams 0.1 s + Complete. Pass: C's PlaybackComplete ≥ A's audio end (A's first chunk + 3 s).
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
		return c, watch(sctx, c, name), scancel, sendPrepare(c)
	}

	a, _, closeA, err := open("A")
	aStart := time.Now()
	if err == nil {
		err = sendZeros(a, raceChunks, 16*time.Millisecond)
	}
	if err == nil {
		err = sendComplete(a)
	}
	if closeA != nil {
		closeA()
	}
	if err != nil {
		return fmt.Errorf("A: %w", err)
	}
	fmt.Println(ts(), "A streamed 3 s at 2×, completed and closed")

	admitted := false
	for try := 1; try <= raceTries && !admitted; try++ {
		b, ev, closeB, err := open("B")
		if b == nil {
			return err
		}
		if err == nil {
			time.Sleep(100 * time.Millisecond)
			admitted, err = bAdmitted(ev)
		}
		fmt.Printf("%s B try %d admitted=%v (err %v), closing\n", ts(), try, admitted, err)
		closeB()
		if err != nil {
			return fmt.Errorf("race: B (try %d): %w", try, err)
		}
		if !admitted {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if !admitted {
		return fmt.Errorf("race: INCONCLUSIVE B was never admitted in %d tries, so its close tested nothing", raceTries)
	}

	for try := 1; ; try++ {
		c, ev, closeC, err := open("C")
		if c == nil {
			return fmt.Errorf("race: C (try %d): %w", try, err)
		}
		if err == nil {
			err = sendZeros(c, 3, 32*time.Millisecond)
		}
		var sent time.Time
		if err == nil {
			sent = time.Now()
			err = sendComplete(c)
		}
		e, act, ok := verdictAfter(ctx, ev, sent)
		closeC()
		switch {
		case !ok:
			return fmt.Errorf("race: C (try %d) got no verdict (send err %v)", try, err)
		case act == retry:
			fmt.Printf("%s C try %d refused (%v %T), retrying\n", ts(), try, e.err, e.r.GetAudioResponseType())
			time.Sleep(200 * time.Millisecond)
			continue
		case e.r.GetAudioStreamPlaybackComplete() == nil:
			return fmt.Errorf("race: C (try %d) ended %v %T", try, e.err, e.r.GetAudioResponseType())
		}
		margin := raceMargin(aStart, e.at)
		if margin < 0 {
			return fmt.Errorf("race: FAIL C's PlaybackComplete %s before A's audio end (A's playback was cut)", (-margin).Round(time.Millisecond))
		}
		fmt.Printf("%s race: PASS C's PlaybackComplete %s after A's audio end\n", ts(), margin.Round(time.Millisecond))
		return nil
	}
}

// raceMargin is how far at lands past A's audio end: aStart (A's first chunk) + raceChunks × 32 ms.
func raceMargin(aStart, at time.Time) time.Duration {
	return at.Sub(aStart.Add(raceChunks * 32 * time.Millisecond))
}

// bAdmitted reads B's first event, if any: ResourceExhausted is a refusal, silence or a response is admission.
func bAdmitted(ev <-chan event) (bool, error) {
	select {
	case e, more := <-ev:
		switch {
		case !more:
			return false, errors.New("stream closed without an event")
		case e.err == nil:
			return true, nil
		case status.Code(e.err) == codes.ResourceExhausted:
			return false, nil
		}
		return false, e.err
	default:
		return true, nil
	}
}

type action int

const (
	skip    action = iota
	retry          // a refusal: close C, retry
	verdict        // C's outcome
)

// classify is C's action on e, given sent = when C's Complete went out (zero: not sent). Before sent, a
// PlaybackComplete is an earlier playback's broadcast and a Failure/Overrun a refusal or stale broadcast.
func classify(e event, sent time.Time) action {
	early := sent.IsZero() || e.at.Before(sent)
	switch {
	case e.err != nil && status.Code(e.err) == codes.ResourceExhausted:
		return retry
	case e.err != nil:
		return verdict
	case e.r.GetAudioStreamPlaybackFailyer() != nil, e.r.GetAudioStreamBufferOverrun() != nil:
		if early {
			return retry
		}
		return verdict
	case e.r.GetAudioStreamPlaybackComplete() != nil:
		if early {
			return skip
		}
		return verdict
	}
	return skip
}

// verdictAfter is C's first non-skip event with its action; false when the stream closes or ctx ends first.
func verdictAfter(ctx context.Context, ev <-chan event, sent time.Time) (event, action, bool) {
	for {
		select {
		case e, more := <-ev:
			if !more {
				return event{}, skip, false
			}
			if act := classify(e, sent); act != skip {
				return e, act, true
			}
		case <-ctx.Done():
			return event{}, skip, false
		}
	}
}
