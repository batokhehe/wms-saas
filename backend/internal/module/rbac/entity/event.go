package entity

import (
	"time"

	"github.com/google/uuid"
)

// Domain events describe things that happened, in the past tense. For RBAC they
// are the access-control audit trail: who changed what anyone is allowed to do.
//
// They live in entity/ because an event is a domain fact, not a transport
// concern. Publishing them is the service's job (see service/publisher.go).
// No handlers are implemented — see the auth module's publisher for why
// publishing into a broker with no subscriber would manufacture alert noise.

// EventName identifies an event type. These strings appear in logs and will
// become queue task names, so they are part of the contract and must not be
// renamed once released.
type EventName string

const (
	EventRoleCreated        EventName = "rbac.role.created"
	EventRoleUpdated        EventName = "rbac.role.updated"
	EventRoleDeleted        EventName = "rbac.role.deleted"
	EventPermissionAssigned EventName = "rbac.permission.assigned"
	EventPermissionRevoked  EventName = "rbac.permission.revoked"
)

// Event is the envelope every RBAC domain event shares.
//
// Every field is prefixed "event_" when logged, so an event's identifiers never
// collide with the request-scoped logger's own company_id and user_id. See
// service/publisher.go.
type Event struct {
	Name EventName

	// CompanyID is the tenant whose authorisation changed.
	CompanyID uuid.UUID

	// ActorID is the user who made the change. Distinct from the RoleID the
	// change was made TO: "who granted whom what" needs both, and conflating
	// them makes the trail useless for the questions it exists to answer.
	ActorID uuid.UUID

	OccurredAt time.Time
	RequestID  string
	Attributes map[string]any
}

// NewEvent builds an event envelope.
func NewEvent(
	name EventName,
	companyID, actorID uuid.UUID,
	occurredAt time.Time,
	requestID string,
) Event {
	return Event{
		Name:       name,
		CompanyID:  companyID,
		ActorID:    actorID,
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
