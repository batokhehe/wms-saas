package port

import "time"

// Clock abstracts the reading of wall-clock time.
//
// Business logic must never call time.Now() directly. A rule that depends on
// "now" — a reservation that expires in 30 minutes, a cut-off at 17:00, an SLA
// breach — is untestable when the clock is a package-level function: the test
// either sleeps, or asserts on a value it cannot control, or is skipped.
//
// With a Clock, the same rule is exercised at any instant the test chooses,
// including the awkward ones: a leap day, the moment a daylight-saving shift
// lands, the second either side of a deadline.
type Clock interface {
	// Now returns the current time. Implementations must return UTC.
	Now() time.Time
}

// ClockFunc adapts a plain function into a Clock.
type ClockFunc func() time.Time

// Now implements Clock.
func (f ClockFunc) Now() time.Time { return f() }
