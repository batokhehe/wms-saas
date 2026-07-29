package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The inventory event catalogue. Every transition raises exactly one of these,
// FROM the aggregate — never from a service. A consumer materialising a read
// model or an audit trail can reconstruct a position's whole history from this
// stream alone.
const (
	EventPositionCreated EventName = "inventory.position_created"

	EventStockReceived EventName = "inventory.stock_received"
	EventStockIssued   EventName = "inventory.stock_issued"

	EventStockReserved    EventName = "inventory.stock_reserved"
	EventStockReleased    EventName = "inventory.stock_released"
	EventStockAllocated   EventName = "inventory.stock_allocated"
	EventStockDeallocated EventName = "inventory.stock_deallocated"

	EventStockQuarantined            EventName = "inventory.stock_quarantined"
	EventStockReleasedFromQuarantine EventName = "inventory.stock_released_from_quarantine"

	EventStockAdjusted EventName = "inventory.stock_adjusted"
)

// Event is an immutable record that a stock transition happened.
//
// Attributes carry only immutable FACTS — primitives and strings, never a
// pointer into the aggregate. Every quantity event is stamped with the resulting
// four balances and the derived on-hand, so a read model never has to reload the
// aggregate to know where the position landed.
type Event struct {
	Name EventName
	PositionID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (p *InventoryPosition) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	p.events = append(p.events, Event{
		Name:       name,
		PositionID: p.id,
		CompanyID:  p.key.CompanyID(),
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer. Clearing on read makes a double publish impossible.
func (p *InventoryPosition) PullEvents() []Event {
	out := p.events
	p.events = nil
	return out
}

// state renders the resulting balances, stamped on every quantity event.
func (p *InventoryPosition) state() map[string]any {
	return map[string]any{
		"available":   p.available.Value(),
		"reserved":    p.reserved.Value(),
		"allocated":   p.allocated.Value(),
		"quarantined": p.quarantined.Value(),
		"on_hand":     p.OnHand(),
	}
}

// withState merges the resulting balances into a set of event-specific facts.
func (p *InventoryPosition) withState(facts map[string]any) map[string]any {
	for k, v := range p.state() {
		facts[k] = v
	}
	return facts
}
