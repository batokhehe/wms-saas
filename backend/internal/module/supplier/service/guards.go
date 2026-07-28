package service

import (
	"context"

	"github.com/google/uuid"
)

// DeletionGuard is the PREPARED extension point for the invariant "a supplier
// cannot be deleted while referenced by a purchase order".
//
// # Why it is only prepared, not consumed
//
// This sprint implements Create/Update/Activate/Deactivate — there is NO delete
// behaviour, because a supplier is retired by Deactivate, not erased. The
// interface exists so the future Purchase Order sprint has a seam: it will
// implement CanDelete ("return CONFLICT when open purchase orders reference this
// supplier") and a delete operation will consult it before the aggregate, in the
// exact shape warehouse's DeletionGuard uses for Archive. Until then, this is a
// declared contract with a permissive default and no caller.
//
// It is deliberately NOT injected into the Service yet: wiring a guard that no
// operation consults would be dead code. Declaring the interface and its default
// here is what "prepare the extension point only" means.
type DeletionGuard interface {
	// CanDelete returns nil when a supplier may be deleted, or a typed error
	// explaining why not. It takes a companyID as well as the supplier id so an
	// implementation cannot accidentally query across tenants.
	CanDelete(ctx context.Context, companyID, supplierID uuid.UUID) error
}

// AllowAllDeletion permits every deletion. It is the implementation that would be
// in force until the Purchase Order sprint exists — a named type rather than a
// nil check, so an unwired guard could never be mistaken for a permissive one.
type AllowAllDeletion struct{}

var _ DeletionGuard = (*AllowAllDeletion)(nil)

// NewAllowAllDeletion builds the permissive guard.
func NewAllowAllDeletion() *AllowAllDeletion { return &AllowAllDeletion{} }

// CanDelete always permits.
func (AllowAllDeletion) CanDelete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
