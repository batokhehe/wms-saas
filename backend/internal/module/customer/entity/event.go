package entity

import (
	"time"

	"github.com/google/uuid"
)

// EventName is a stable, machine-readable domain event identifier.
type EventName string

// The customer event catalogue. Each is raised BY the aggregate.
const (
	EventCustomerCreated     EventName = "customer.created"
	EventCustomerUpdated     EventName = "customer.updated"
	EventCustomerActivated   EventName = "customer.activated"
	EventCustomerDeactivated EventName = "customer.deactivated"
)

// Event is an immutable record that a customer transition happened. Attributes
// carry only immutable facts — primitives and strings, never a pointer into the
// aggregate.
type Event struct {
	Name EventName
	CustomerID,
	CompanyID,
	ActorID uuid.UUID
	OccurredAt time.Time
	Attributes map[string]any
}

// record appends an event with the aggregate's identity already filled in.
func (c *Customer) record(name EventName, actor uuid.UUID, now time.Time, attributes map[string]any) {
	c.events = append(c.events, Event{
		Name:       name,
		CustomerID: c.id,
		CompanyID:  c.companyID,
		ActorID:    actor,
		OccurredAt: now,
		Attributes: attributes,
	})
}

// PullEvents returns the events recorded since the last pull and clears the
// buffer, so a double publish is impossible.
func (c *Customer) PullEvents() []Event {
	out := c.events
	c.events = nil
	return out
}
