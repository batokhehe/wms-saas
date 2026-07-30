package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The stock-transfer event catalogue. Each is raised BY the aggregate — never by
// a service — so an event exists because a transition happened.
const (
	EventTransferCreated   EventName = "stocktransfer.created"
	EventTransferUpdated   EventName = "stocktransfer.updated"
	EventTransferConfirmed EventName = "stocktransfer.confirmed"
	EventTransferCompleted EventName = "stocktransfer.completed"
	EventTransferCancelled EventName = "stocktransfer.cancelled"
)

// Event is an immutable record that a transfer transition happened. Attributes
// carry only immutable facts — primitives and strings, never a pointer into the
// aggregate.
type Event struct {
	Name EventName
	TransferID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (t *StockTransfer) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	t.events = append(t.events, Event{
		Name:       name,
		TransferID: t.id,
		CompanyID:  t.companyID,
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer, so a double publish is impossible.
func (t *StockTransfer) PullEvents() []Event {
	out := t.events
	t.events = nil
	return out
}
