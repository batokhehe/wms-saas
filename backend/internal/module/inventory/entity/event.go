package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The inventory event catalogue. Every stock transition raises exactly one of
// these, FROM the aggregate — never from a service. A consumer that materialises
// a read model or an audit trail can reconstruct the whole history of a stock
// position from this stream alone.
const (
	EventInventoryCreated     EventName = "inventory.created"
	EventInventoryIncreased   EventName = "inventory.increased"
	EventInventoryDecreased   EventName = "inventory.decreased"
	EventInventoryReserved    EventName = "inventory.reserved"
	EventInventoryReleased    EventName = "inventory.released"
	EventInventoryAdjusted    EventName = "inventory.adjusted"
	EventInventoryTransferred EventName = "inventory.transferred"
	EventInventoryLocked      EventName = "inventory.locked"
	EventInventoryUnlocked    EventName = "inventory.unlocked"
	EventCycleCountCompleted  EventName = "inventory.cycle_count_completed"
)

// Transfer direction values carried on an EventInventoryTransferred payload.
const (
	TransferDirectionOut = "OUT"
	TransferDirectionIn  = "IN"
)

// Event is an immutable record that a stock transition happened.
//
// Attributes carry only immutable FACTS — primitives and strings, never a
// pointer into the aggregate or a value object a consumer cannot serialise. The
// resulting on-hand / reserved / available counts are stamped on every quantity
// event so a read model never has to reload the aggregate to know where the
// position landed.
type Event struct {
	Name EventName
	InventoryID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (i *Inventory) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	i.events = append(i.events, Event{
		Name:        name,
		InventoryID: i.id,
		CompanyID:   i.companyID,
		ActorID:     actor,
		OccurredAt:  now,
		Attributes:  attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer. Clearing on read means a double publish is impossible, and an event
// exists exactly when a transition happened.
func (i *Inventory) PullEvents() []Event {
	out := i.events
	i.events = nil
	return out
}

// state renders the resulting counts, stamped on every quantity event.
func (i *Inventory) state() map[string]any {
	return map[string]any{
		"on_hand":   i.onHand.Value(),
		"reserved":  i.reserved.Value(),
		"available": i.Available().Value(),
	}
}

// withState merges the resulting counts into a set of event-specific facts.
func (i *Inventory) withState(facts map[string]any) map[string]any {
	for k, v := range i.state() {
		facts[k] = v
	}
	return facts
}
