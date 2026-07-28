package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The supplier event catalogue. Each is raised BY the aggregate — never by a
// service — so an event exists because a transition happened.
const (
	EventSupplierCreated     EventName = "supplier.created"
	EventSupplierUpdated     EventName = "supplier.updated"
	EventSupplierActivated   EventName = "supplier.activated"
	EventSupplierDeactivated EventName = "supplier.deactivated"
)

// Event is an immutable record that a supplier transition happened. Attributes
// carry only immutable facts — primitives and strings, never a pointer into the
// aggregate.
type Event struct {
	Name EventName
	SupplierID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (s *Supplier) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	s.events = append(s.events, Event{
		Name:       name,
		SupplierID: s.id,
		CompanyID:  s.companyID,
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer, so a double publish is impossible.
func (s *Supplier) PullEvents() []Event {
	out := s.events
	s.events = nil
	return out
}
