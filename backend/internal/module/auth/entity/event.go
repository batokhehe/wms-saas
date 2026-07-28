package entity

import (
	"time"

	"github.com/google/uuid"
)

// Domain events describe things that happened, in the past tense. They are the
// audit trail of identity: who authenticated, when, from where.
//
// They live in entity/ because an event is a domain fact, not a transport
// concern. Publishing them is the service's job (see service/publisher.go).
//
// No handlers are implemented in this sprint. Publishing first is deliberate:
// the events become part of the module's contract now, so a Sprint-2 consumer
// is written against a stable shape instead of one retrofitted around whatever
// the code happened to record.

// EventName identifies an event type. These strings appear in logs and will
// become queue task names, so they are part of the contract and must not be
// renamed once released.
type EventName string

const (
	EventUserRegistered      EventName = "auth.user.registered"
	EventUserLoggedIn        EventName = "auth.user.logged_in"
	EventUserLoggedOut       EventName = "auth.user.logged_out"
	EventPasswordChanged     EventName = "auth.password.changed"
	EventRefreshTokenRotated EventName = "auth.refresh_token.rotated"
)

// Event is the envelope every domain event shares.
//
// Note what is absent: no token, no token hash, no password, no email. An audit
// record is written to a log and forwarded to systems with different access
// controls than the database, so it carries identifiers and never secrets.
// Email is a personal identifier and is resolvable from UserID when needed,
// which keeps the audit stream out of GDPR scope.
type Event struct {
	Name EventName
	// UserID is the subject of the event.
	UserID uuid.UUID
	// OccurredAt comes from the injected clock, so event ordering is
	// deterministic in tests.
	OccurredAt time.Time
	// RequestID ties the event to the HTTP request that caused it, so an audit
	// entry can be correlated with the access log.
	RequestID string
	// Attributes carries event-specific, non-sensitive context.
	Attributes map[string]any
}

// NewEvent builds an event envelope.
func NewEvent(name EventName, userID uuid.UUID, occurredAt time.Time, requestID string) Event {
	return Event{
		Name:       name,
		UserID:     userID,
		OccurredAt: occurredAt,
		RequestID:  requestID,
		Attributes: map[string]any{},
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
