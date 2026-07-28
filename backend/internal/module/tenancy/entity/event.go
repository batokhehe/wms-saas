package entity

import (
	"time"

	"github.com/google/uuid"
)

// Domain events describe things that happened, in the past tense. They are the
// audit trail of tenancy: who created a company, who joined it, who left.
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
	EventCompanyCreated  EventName = "tenancy.company.created"
	EventCompanyUpdated  EventName = "tenancy.company.updated"
	EventCompanyDeleted  EventName = "tenancy.company.deleted"
	EventMemberInvited   EventName = "tenancy.member.invited"
	EventMemberRemoved   EventName = "tenancy.member.removed"
	EventCompanySwitched EventName = "tenancy.company.switched"
)

// Event is the envelope every tenancy domain event shares.
//
// Unlike the auth module's event, this one carries CompanyID: a tenancy audit
// record is meaningless without knowing which tenant it concerns, and a
// compliance query ("show me everything that happened in company X") is the
// primary reason these records exist.
//
// It still carries no personal data — no email, no name. Those are resolvable
// from the identifiers when genuinely needed, which keeps the audit stream out
// of GDPR scope.
type Event struct {
	Name EventName

	// CompanyID is the tenant the event concerns.
	CompanyID uuid.UUID

	// ActorID is the user who performed the action. It is distinct from any
	// subject in Attributes: "admin removed staff member" needs both, and
	// conflating them makes the audit trail unusable for exactly the questions
	// it exists to answer.
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
