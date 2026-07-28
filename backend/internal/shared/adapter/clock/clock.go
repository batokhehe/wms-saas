// Package clock implements port.Clock.
//
// It ships two implementations: System for production and Fake for tests. The
// fake lives in the production package rather than a _test.go file so that any
// module's tests can import it, which is the whole reason the abstraction
// exists.
package clock

import (
	"sync"
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
)

// System reads the real wall clock.
type System struct{}

var _ port.Clock = (*System)(nil)

// NewSystem builds the production clock.
func NewSystem() *System { return &System{} }

// Now returns the current time in UTC.
//
// UTC is not a formatting preference. A WMS runs warehouses in different time
// zones, and storing local time means a stock movement recorded at 01:30 during
// a daylight-saving rollback is ambiguous: it happened twice. Every timestamp
// is UTC in the database, converted for display at the edge.
func (System) Now() time.Time { return time.Now().UTC() }

// Fake is a controllable clock for tests.
//
// It is safe for concurrent use, because code under test frequently reads the
// clock from several goroutines while the test advances it from another.
type Fake struct {
	mu  sync.RWMutex
	now time.Time
}

var _ port.Clock = (*Fake)(nil)

// NewFake builds a clock fixed at the given instant, normalised to UTC.
func NewFake(at time.Time) *Fake {
	return &Fake{now: at.UTC()}
}

// NewFakeAt builds a clock from an RFC3339 string, panicking on a malformed
// input. It is a test helper, so panicking is appropriate: a bad literal is a
// bug in the test itself and should fail immediately and loudly.
func NewFakeAt(rfc3339 string) *Fake {
	parsed, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic("clock: NewFakeAt: " + err.Error())
	}
	return NewFake(parsed)
}

// Now returns the fixed time.
func (f *Fake) Now() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.now
}

// Advance moves the clock forward. A negative duration moves it backward, which
// is occasionally what a test of clock-skew handling needs.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves the clock to an absolute instant.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}
