package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDue(t *testing.T) {
	tl, err := time.LoadLocation("Europe/Tallinn")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, tl) }
	for _, c := range []struct {
		name         string
		now, lastRun time.Time
		want         bool
	}{
		{"03:59 not due", at(8, 3, 59), at(7, 5, 0), false},
		{"04:00 due", at(8, 4, 0), at(7, 5, 0), true},
		{"ran yesterday, start 09:00", at(8, 9, 0), at(7, 5, 0), true},
		{"ran today 04:30", at(8, 9, 0), at(8, 4, 30), false},
		{"never ran", at(8, 3, 0), time.Time{}, true},
		{"04:00 Tallinn is 02:00 UTC", time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC), at(7, 5, 0), true},
	} {
		if got := Due(c.now, c.lastRun, tl); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}

func usePass(t *testing.T, err *error) *int {
	t.Helper()
	runs, oldPass, oldNow, oldFailed := 0, Pass, Now, failedAt
	Pass, failedAt = func(context.Context) error { runs++; return *err }, time.Time{}
	t.Cleanup(func() { Pass, Now, failedAt = oldPass, oldNow, oldFailed; Touch() })
	return &runs
}

func TestDueIdle(t *testing.T) {
	useEpisodes(t, `{}`)
	logs := useModel(t, `{}`)
	var fail error
	runs := usePass(t, &fail)
	t0 := time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC)
	clock := func(d time.Duration) { Now = func() time.Time { return t0.Add(d) } }

	clock(0)
	Touch()
	clock(IdleFor - time.Second)
	if Tick(time.UTC) || *runs != 0 {
		t.Fatalf("29m59s idle: Pass called %d times, want 0", *runs)
	}
	fail = errors.New("ollama down")
	clock(IdleFor)
	if !Tick(time.UTC) || *runs != 1 {
		t.Fatalf("30m idle: Pass called %d times, want 1", *runs)
	}
	if len(*logs) != 1 {
		t.Fatalf("failed pass logs %q, want one line", *logs)
	}
	clock(IdleFor + RetryAfter - time.Minute)
	if Tick(time.UTC) || *runs != 1 {
		t.Fatalf("59m after a failure: Pass called %d times, want 1", *runs)
	}
	fail = nil
	clock(IdleFor + RetryAfter)
	if !Tick(time.UTC) || *runs != 2 {
		t.Fatalf("1h after a failure: Pass called %d times, want 2", *runs)
	}
}

func TestTickOff(t *testing.T) {
	useEpisodes(t, `{"memory_model":"off"}`)
	var fail error
	runs := usePass(t, &fail)
	Now = func() time.Time { return time.Date(2026, 11, 8, 5, 0, 0, 0, time.UTC) }
	turnMu.Lock()
	lastTurn = time.Time{}
	turnMu.Unlock()
	if Tick(time.UTC) || *runs != 0 {
		t.Fatalf("off: Pass called %d times, want 0", *runs)
	}
}
