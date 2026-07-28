// Package id implements port.IDGenerator.
//
// It ships UUID for production and Sequential for tests. The deterministic
// generator lives in the production package so any module's tests can import
// it — the point of the abstraction is that a test can predict the identifiers
// its code will produce.
package id

import (
	"fmt"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
)

// UUID generates random UUIDv4 identifiers.
type UUID struct{}

var _ port.IDGenerator = (*UUID)(nil)

// NewUUID builds the production generator.
func NewUUID() *UUID { return &UUID{} }

// NewID returns a new UUIDv4.
//
// uuid.New panics if the system entropy source fails. That is the correct
// behaviour and is why it is not wrapped: a process that cannot read randomness
// must not continue issuing identifiers, because the alternative is silently
// emitting predictable or colliding ids.
func (UUID) NewID() uuid.UUID { return uuid.New() }

// Sequential generates predictable identifiers for tests.
//
// The n-th call returns 00000000-0000-4000-8000-0000000000NN, which is a
// structurally valid v4 UUID. Tests can therefore assert on exact identifiers:
//
//	gen := id.NewSequential()
//	want := gen.Peek(1)   // the id the next call will produce
//
// It is safe for concurrent use, though a test relying on a specific ordering
// across goroutines is testing the wrong thing.
type Sequential struct {
	counter atomic.Uint64
}

var _ port.IDGenerator = (*Sequential)(nil)

// NewSequential builds a deterministic generator starting at 1.
func NewSequential() *Sequential { return &Sequential{} }

// NewID returns the next identifier in the sequence.
func (s *Sequential) NewID() uuid.UUID {
	return buildSequentialUUID(s.counter.Add(1))
}

// Peek returns the identifier the n-th call will produce, without consuming
// anything. It lets a test state its expectation before exercising the code.
func (s *Sequential) Peek(n uint64) uuid.UUID {
	return buildSequentialUUID(s.counter.Load() + n)
}

// buildSequentialUUID renders a counter as a valid v4 UUID.
//
// Version (4) and variant (8) nibbles are set so the result passes uuid.Parse
// and any `binding:"uuid4"` validation — a fake that produces malformed ids
// would make tests fail for reasons unrelated to what they assert.
func buildSequentialUUID(n uint64) uuid.UUID {
	parsed, err := uuid.Parse(fmt.Sprintf("00000000-0000-4000-8000-%012d", n))
	if err != nil {
		// Unreachable: the format string always yields a valid UUID. Panicking
		// beats returning uuid.Nil, which would silently break test assertions.
		panic("id: building sequential uuid: " + err.Error())
	}
	return parsed
}
