package timex

import (
	"testing"
	"time"
)

func TestNowUsesInjectedClock(t *testing.T) {
	want := time.Date(2026, 4, 27, 12, 13, 14, 0, time.UTC)

	got := Now(func() time.Time { return want })

	if !got.Equal(want) {
		t.Fatalf("Now() = %s, want %s", got, want)
	}
}

func TestNowFallsBackToWallClock(t *testing.T) {
	before := time.Now()
	got := Now(nil)
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Fatalf("Now(nil) = %s, want between %s and %s", got, before, after)
	}
}
