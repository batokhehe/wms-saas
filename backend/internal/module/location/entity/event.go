package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Domain events describe what happened to a location, in the past tense.
//
// Like the Warehouse aggregate's, these are RAISED BY THE AGGREGATE rather than
// by the service: the aggregate knows a transition occurred because it
// performed it, and it cannot forget. The service publishes what it pulls.

// EventName identifies an event type. These strings appear in logs and will
// become queue task names, so they are part of the contract and must not be
// renamed once released.
type EventName string

const (
	EventLocationCreated  EventName = "location.created"
	EventLocationLocked   EventName = "location.locked"
	EventLocationUnlocked EventName = "location.unlocked"
	EventCapacityChanged  EventName = "location.capacity_changed"
	EventBarcodeAssigned  EventName = "location.barcode_assigned"
)

// Event is a location domain fact.
//
// It carries the warehouse as well as the location and tenant. A consumer
// reacting to a lock — a putaway planner rebuilding its candidate set, say —
// needs to know which site is affected, and making it load the location to find
// out would defeat the point of an event being self-contained.
type Event struct {
	Name EventName

	LocationID  uuid.UUID
	WarehouseID uuid.UUID
	CompanyID   uuid.UUID

	// ActorID is the user who caused it. Distinct from LocationID: "who locked
	// which bin" needs both.
	ActorID uuid.UUID

	OccurredAt time.Time
	Attributes map[string]any
}

// newEvent builds an event from the aggregate's current state.
//
// Unexported: only the aggregate may raise its own events. A service that could
// construct one would be able to claim a transition happened that never did.
func newEvent(
	name EventName, l *StorageLocation, actorID uuid.UUID, occurredAt time.Time,
) Event {
	return Event{
		Name:        name,
		LocationID:  l.id,
		WarehouseID: l.warehouseID,
		CompanyID:   l.companyID,
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

// trimReason normalises a free-text reason supplied with a lock.
func trimReason(raw string) string { return strings.TrimSpace(raw) }
