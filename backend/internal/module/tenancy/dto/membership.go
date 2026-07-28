package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ---------- Requests ----------

// InviteMemberRequest adds a person to the active company.
//
// It names the invitee by EMAIL, not by user id. Two reasons:
//
//  1. The inviter knows their colleague's email address, not their internal
//     UUID. An API that demanded the id would force the client to expose a user
//     search endpoint — which is an account enumeration oracle.
//  2. It keeps the tenancy module from needing to browse the identity module's
//     data. It asks "who has this address?" through a narrow interface and gets
//     one answer or none.
//
// CompanyID is deliberately absent: the target company comes from the request
// context, so a client cannot invite someone into a company it does not have an
// active membership in.
type InviteMemberRequest struct {
	Email string `json:"email" binding:"required,email,max=255"`

	// Role is constrained by tag to the three known values. OWNER is permitted
	// here but rejected by the validator — see validator.ValidateInvite for why
	// ownership is a transfer rather than an invitation.
	Role string `json:"role" binding:"required,oneof=OWNER ADMIN STAFF"`
}

// ListMembershipsQuery is the member list query string.
type ListMembershipsQuery struct {
	pagination.Request

	Status string `form:"status" binding:"omitempty,oneof=ACTIVE PENDING SUSPENDED"`
	Role   string `form:"role"   binding:"omitempty,oneof=OWNER ADMIN STAFF"`
}

// MembershipSortOptions declares this endpoint's paging rules.
func MembershipSortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"role":       "memberships.role",
			"status":     "memberships.status",
			"joined_at":  "memberships.joined_at",
			"created_at": "memberships.created_at",
		},
	}
}

// ---------- Responses ----------

// MembershipResponse is the public representation of one grant of access.
type MembershipResponse struct {
	ID        uuid.UUID  `json:"id"`
	CompanyID uuid.UUID  `json:"company_id"`
	UserID    uuid.UUID  `json:"user_id"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	JoinedAt  *time.Time `json:"joined_at,omitempty"`
	InvitedBy *uuid.UUID `json:"invited_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// MembershipWithCompanyResponse pairs a membership with the company it grants
// access to.
//
// This backs the "which companies can I switch to?" view. Returning the company
// inline rather than as an id avoids the client making one request per
// membership to render a switcher menu.
type MembershipWithCompanyResponse struct {
	MembershipID uuid.UUID       `json:"membership_id"`
	Role         string          `json:"role"`
	Status       string          `json:"status"`
	JoinedAt     *time.Time      `json:"joined_at,omitempty"`
	Company      CompanyResponse `json:"company"`
}
