package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The purchase-order event catalogue. Each is raised BY the aggregate — never by
// a service — so an event exists because a transition happened.
const (
	EventOrderCreated         EventName = "purchaseorder.created"
	EventOrderUpdated         EventName = "purchaseorder.updated"
	EventOrderApproved        EventName = "purchaseorder.approved"
	EventOrderCancelled       EventName = "purchaseorder.cancelled"
	EventOrderReceiptRecorded EventName = "purchaseorder.receipt_recorded"
	EventOrderCompleted       EventName = "purchaseorder.completed"
)

// Event is an immutable record that a purchase-order transition happened.
// Attributes carry only immutable facts — primitives and strings, never a
// pointer into the aggregate.
type Event struct {
	Name EventName
	OrderID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (o *PurchaseOrder) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	o.events = append(o.events, Event{
		Name:       name,
		OrderID:    o.id,
		CompanyID:  o.companyID,
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer, so a double publish is impossible.
func (o *PurchaseOrder) PullEvents() []Event {
	out := o.events
	o.events = nil
	return out
}
