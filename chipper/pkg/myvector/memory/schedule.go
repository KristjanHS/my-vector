package memory

import (
	"context"
	"errors"
	"time"
)

const (
	// RunHour is the local hour a nightly pass becomes due (M20).
	RunHour = 4
	// IdleFor is how long no turn must have landed before a due pass starts.
	IdleFor = 30 * time.Minute
	// RetryAfter spaces scheduled attempts after a failed pass.
	RetryAfter = time.Hour
)

// failedAt is the last failed scheduled pass; Tick's goroutine only.
var failedAt time.Time

// Due: lastRun is before the latest RunHour:00 in zone at or before now — a missed night is due at the next start.
func Due(now, lastRun time.Time, zone *time.Location) bool {
	n := now.In(zone)
	cut := time.Date(n.Year(), n.Month(), n.Day(), RunHour, 0, 0, 0, zone)
	if n.Before(cut) {
		cut = cut.AddDate(0, 0, -1)
	}
	return lastRun.Before(cut)
}

// Tick runs Pass when not off, Due, idle IdleFor and RetryAfter past the last failure; true = Pass was called.
func Tick(zone *time.Location) bool {
	now := Now()
	if Off() || now.Sub(LastTurn()) < IdleFor || now.Sub(failedAt) < RetryAfter {
		return false
	}
	s, err := Read()
	if err != nil {
		logf("myvector: memory schedule read:", err)
		return false
	}
	if !Due(now, s.LastRun, zone) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	defer cancel()
	if err := Pass(ctx); err != nil && !errors.Is(err, ErrBusy) {
		failedAt = now
		logf("myvector: memory scheduled pass failed, retry in", RetryAfter, ":", err)
	} else if err == nil {
		failedAt = time.Time{}
	}
	return true
}

// Loop ticks every minute with zone() as the day boundary (M21).
func Loop(zone func() *time.Location) {
	for range time.Tick(time.Minute) {
		Tick(zone())
	}
}
