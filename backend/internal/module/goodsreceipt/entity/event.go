package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The goods-receipt event catalogue. Each is raised BY the aggregate — never by
// a service — so an event exists because a transition happened.
const (
	EventReceiptCreated   EventName = "goodsreceipt.created"
	EventReceiptUpdated   EventName = "goodsreceipt.updated"
	EventReceiptConfirmed EventName = "goodsreceipt.confirmed"
	EventReceiptReceived  EventName = "goodsreceipt.received"
	EventReceiptCancelled EventName = "goodsreceipt.cancelled"
)

// Event is an immutable record that a goods-receipt transition happened.
// Attributes carry only immutable facts — primitives and strings, never a
// pointer into the aggregate.
type Event struct {
	Name EventName
	ReceiptID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (g *GoodsReceipt) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	g.events = append(g.events, Event{
		Name:       name,
		ReceiptID:  g.id,
		CompanyID:  g.companyID,
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer, so a double publish is impossible.
func (g *GoodsReceipt) PullEvents() []Event {
	out := g.events
	g.events = nil
	return out
}
