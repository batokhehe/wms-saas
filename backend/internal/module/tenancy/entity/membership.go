package entity

import (
	"time"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// Role is a member's role within one company.
//
// Note the wording: WITHIN ONE COMPANY. Role is a property of the relationship,
// not of the person. The same human can be OWNER of their own company and STAFF
// at a client's, which is precisely why it lives on Membership and could never
// live on User.
type Role string

const (
	// RoleOwner created the company, or inherited it. Cannot be removed while
	// they are the last owner.
	RoleOwner Role = "OWNER"
	// RoleAdmin manages members and settings.
	RoleAdmin Role = "ADMIN"
	// RoleStaff performs day-to-day warehouse operations.
	RoleStaff Role = "STAFF"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleStaff:
		return true
	default:
		return false
	}
}

// MembershipStatus is the lifecycle state of a membership.
type MembershipStatus string

const (
	// MembershipActive can be used to resolve a company context.
	MembershipActive MembershipStatus = "ACTIVE"

	// MembershipPending is an issued but unaccepted invitation.
	//
	// The row exists so the invitation is durable and the seat is reserved, but
	// it grants nothing until accepted. This is what lets an invitation survive
	// a server restart without a separate invitations table.
	MembershipPending MembershipStatus = "PENDING"

	// MembershipSuspended is revoked access without losing the record. The
	// history of what this person did stays attributable.
	MembershipSuspended MembershipStatus = "SUSPENDED"
)

// Valid reports whether s is a known status.
func (s MembershipStatus) Valid() bool {
	switch s {
	case MembershipActive, MembershipPending, MembershipSuspended:
		return true
	default:
		return false
	}
}

// Membership joins a user to a company with a role.
//
// It is the ONLY link between the two. users has no company_id and companies
// has no owner_id, so this is the single source of truth for who belongs where.
type Membership struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`

	// Role is stored but NOT enforced anywhere yet — RBAC is the next sprint.
	// The column exists now so that enabling permission checks is a change to
	// the authorisation layer rather than a migration on a populated table.
	Role Role `gorm:"type:varchar(16);not null"`

	Status MembershipStatus `gorm:"type:varchar(16);not null;default:PENDING"`

	// JoinedAt is set when the membership becomes ACTIVE. Nil while PENDING:
	// "invited Monday, joined Friday" is a real and auditable distinction, so
	// defaulting it to CreatedAt would destroy information.
	JoinedAt *time.Time

	// InvitedBy is nil for the OWNER membership created during company
	// registration, because nobody invited the founder.
	InvitedBy *uuid.UUID `gorm:"type:uuid"`
}

// TableName pins the table name.
func (Membership) TableName() string { return "memberships" }

// CanAccess reports whether this membership grants a usable company context.
//
// Only ACTIVE does. A PENDING invitation and a SUSPENDED member both resolve to
// false, which is what stops an un-accepted invite being used as an access
// grant — the single most important predicate in the tenancy model.
func (m *Membership) CanAccess() bool { return m.Status == MembershipActive }

// IsOwner reports whether this membership is an ownership grant.
func (m *Membership) IsOwner() bool { return m.Role == RoleOwner }

// IsPending reports whether the invitation has not yet been accepted.
func (m *Membership) IsPending() bool { return m.Status == MembershipPending }

// Activate transitions a membership to ACTIVE and stamps the join time.
//
// The clock is passed in rather than read from time.Now(), so a test can pin
// it. Idempotent: re-activating preserves the original JoinedAt, so the audit
// trail records when the person actually joined rather than when someone last
// touched the row.
func (m *Membership) Activate(now time.Time) {
	m.Status = MembershipActive
	if m.JoinedAt == nil {
		m.JoinedAt = &now
	}
}

// MembershipID identifies one grant of access.
type MembershipID = uuid.UUID
