package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystemReturnsUTC(t *testing.T) {
	got := NewSystem().Now()

	if got.Location() != time.UTC {
		t.Errorf("Now().Location() = %v, want UTC", got.Location())
	}
}

func TestFakeIsFixed(t *testing.T) {
	c := NewFakeAt("2026-07-22T10:00:00Z")

	first := c.Now()
	second := c.Now()

	if !first.Equal(second) {
		t.Errorf("a fake clock moved on its own: %v then %v", first, second)
	}
}

func TestFakeAdvance(t *testing.T) {
	c := NewFakeAt("2026-07-22T10:00:00Z")

	c.Advance(90 * time.Minute)

	if got, want := c.Now().Format(time.RFC3339), "2026-07-22T11:30:00Z"; got != want {
		t.Errorf("Now() = %s, want %s", got, want)
	}
}

// TestFakeNormalisesToUTC matters because a test that constructs a clock in
// local time would otherwise produce assertions that pass on one developer's
// machine and fail in CI.
func TestFakeNormalisesToUTC(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("tzdata unavailable on this platform")
	}

	c := NewFake(time.Date(2026, 7, 22, 19, 0, 0, 0, tokyo))

	if c.Now().Location() != time.UTC {
		t.Errorf("Now().Location() = %v, want UTC", c.Now().Location())
	}
	if got, want := c.Now().Format(time.RFC3339), "2026-07-22T10:00:00Z"; got != want {
		t.Errorf("Now() = %s, want %s", got, want)
	}
}

// TestFakeIsConcurrencySafe reflects real usage: code under test often reads the
// clock from several goroutines while the test advances it.
func TestFakeIsConcurrencySafe(t *testing.T) {
	c := NewFakeAt("2026-07-22T10:00:00Z")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = c.Now() }()
		go func() { defer wg.Done(); c.Advance(time.Second) }()
	}
	wg.Wait()

	if got, want := c.Now().Format(time.RFC3339), "2026-07-22T10:00:50Z"; got != want {
		t.Errorf("Now() = %s, want %s", got, want)
	}
}
