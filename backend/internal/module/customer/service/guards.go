package service

import (
	"context"

	"github.com/google/uuid"
)

// DeletionGuard is the PREPARED extension point for the invariant "a customer
// cannot be deleted while referenced by a sales order".
//
// # Why it is only prepared, not consumed
//
// This sprint implements Create/Update/Activate/Deactivate — there is NO delete
// behaviour, because a customer is retired by Deactivate, not erased. The
// interface exists so the future Sales Order sprint has a seam: it will implement
// CanDelete ("return CONFLICT when open sales orders reference this customer")
// and a delete operation will consult it before the aggregate, in the exact shape
// warehouse's DeletionGuard uses for Archive. Until then, this is a declared
// contract with a permissive default and no caller — declaring the interface and
// its default here is what "prepare the extension point only" means.
type DeletionGuard interface {
	// CanDelete returns nil when a customer may be deleted, or a typed error. It
	// takes a companyID as well as the customer id so an implementation cannot
	// accidentally query across tenants.
	CanDelete(ctx context.Context, companyID, customerID uuid.UUID) error
}

// AllowAllDeletion permits every deletion — the implementation that would be in
// force until the Sales Order sprint exists. A named type rather than a nil
// check, so an unwired guard could never be mistaken for a permissive one.
type AllowAllDeletion struct{}

var _ DeletionGuard = (*AllowAllDeletion)(nil)

// NewAllowAllDeletion builds the permissive guard.
func NewAllowAllDeletion() *AllowAllDeletion { return &AllowAllDeletion{} }

// CanDelete always permits.
func (AllowAllDeletion) CanDelete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
