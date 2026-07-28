package entity

import (
	"time"

	"github.com/google/uuid"
)

// Domain events describe what happened to a warehouse, in the past tense.
//
// Unlike the other modules' events, these are RAISED BY THE AGGREGATE rather
// than by the service. That is the difference between an event stream that
// records domain facts and one that records what a service layer happened to
// remember to log: the aggregate knows a transition occurred because it
// performed it, and it cannot forget.
//
// The aggregate records; the service publishes. See Warehouse.PullEvents.

// EventName identifies an event type. These strings appear in logs and will
// become queue task names, so they are part of the contract and must not be
// renamed once released.
type EventName string

const (
	EventWarehouseCreated        EventName = "warehouse.created"
	EventWarehouseActivated      EventName = "warehouse.activated"
	EventWarehouseSuspended      EventName = "warehouse.suspended"
	EventWarehouseArchived       EventName = "warehouse.archived"
	EventWarehouseContactChanged EventName = "warehouse.contact_changed"
	EventWarehouseZoneAssigned   EventName = "warehouse.zone_assigned"
)

// Event is a warehouse domain fact.
//
// It carries the aggregate's identity and tenant so a consumer can act on it
// without re-loading the warehouse — the point of an event is that it is
// self-contained.
type Event struct {
	Name EventName

	// WarehouseID is the aggregate the event concerns.
	WarehouseID uuid.UUID

	// CompanyID is the tenant. Every warehouse event is tenant-scoped, and a
	// consumer that had to look it up would defeat the purpose.
	CompanyID uuid.UUID

	// ActorID is the user who caused it. Distinct from WarehouseID: "who
	// suspended which site" needs both.
	ActorID uuid.UUID

	OccurredAt time.Time
	Attributes map[string]any
}

// newEvent builds an event from the aggregate's current state.
//
// Unexported: only the aggregate may raise its own events. A service that could
// construct one would be able to claim a transition happened that never did.
func newEvent(name EventName, w *Warehouse, actorID uuid.UUID, occurredAt time.Time) Event {
	return Event{
		Name:        name,
		WarehouseID: w.id,
		CompanyID:   w.companyID,
		ActorID:     actorID,
		OccurredAt:  occurredAt,
		Attributes:  map[string]any{},
	}
}

// With attaches a non-sensitive attribute and returns the event for chaining.
func (e Event) With(key string, value any) Event {
	if e.Attributes == nil {
		e.Attributes = map[string]any{}
	}
	e.Attributes[key] = value
	return e
}
