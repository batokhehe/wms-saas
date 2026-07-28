package port

import "github.com/google/uuid"

// IDGenerator produces new entity identifiers.
//
// Business modules must never call uuid.New() directly. Two reasons:
//
//  1. Determinism. A test that asserts on a created resource cannot predict a
//     random UUID, so it either ignores the id or reaches into the result to
//     copy it back — both of which weaken the assertion.
//  2. Substitutability. UUIDv4 is random, which makes it a poor B-tree index
//     key: inserts scatter across the whole index and the page cache stops
//     helping. Moving to UUIDv7 (time-ordered) is a one-line change behind this
//     interface and an audit of every package otherwise.
type IDGenerator interface {
	// NewID returns a new unique identifier.
	NewID() uuid.UUID
}

// IDGeneratorFunc adapts a plain function into an IDGenerator.
type IDGeneratorFunc func() uuid.UUID

// NewID implements IDGenerator.
func (f IDGeneratorFunc) NewID() uuid.UUID { return f() }
