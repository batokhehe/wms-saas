// Package service orchestrates the Warehouse aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and, specific to this module,
// NO BUSINESS RULES. Every invariant lives in entity.Warehouse. The service
// loads an aggregate, calls one method on it, persists the result and publishes
// what the aggregate recorded.
//
// The test for whether a rule belongs here: can the aggregate answer it by
// looking only at itself? "Is this warehouse ready to activate?" — yes, that is
// the aggregate's. "Is this code already taken?" — no, that is a question about
// a SET, which only the repository can answer, so the service asks and passes
// the answer in.
package service

import (
	"context"

	"github.com/google/uuid"
)

// DeletionGuard reports whether a warehouse may be archived, considering
// aggregates OUTSIDE the warehouse.
//
// # Why this exists
//
// entity.Warehouse.CanArchive answers what the aggregate can see: "am I already
// archived?". It cannot answer "do I hold stock?", because stock is a different
// aggregate and an aggregate that loaded another would collapse the boundary
// that makes each one independently consistent.
//
// So the cross-aggregate question lives here, as an interface the warehouse
// module declares and a future module implements. The two compose in
// ArchiveWarehouse: ask the guard, then ask the aggregate.
//
// # What the Inventory sprint does
//
// It implements this interface — "return CONFLICT when on-hand quantity is
// non-zero" — and bootstrap injects it in place of AlwaysAllowDeletion. No file
// in this module changes.
//
// The alternative shapes were both worse. A direct call into an inventory
// repository would make warehouse depend on inventory, breaking
// ModuleConvention §6. A domain event consumed asynchronously cannot block the
// operation, and "you archived a warehouse that had stock, sorry" is not a
// usable answer.
type DeletionGuard interface {
	// CanDelete returns nil when archiving is permitted, or a typed error
	// explaining why not.
	//
	// It takes a companyID as well as a warehouse id so an implementation
	// cannot accidentally query across tenants — the same discipline the
	// repositories follow.
	CanDelete(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AlwaysAllowDeletion permits every archive.
//
// This is the implementation in force until Inventory exists. It is a named
// type rather than a nil check in ArchiveWarehouse, because a nil guard would
// mean "no guard configured" and "archiving is allowed" are indistinguishable —
// and a wiring mistake would silently disable a safety check.
type AlwaysAllowDeletion struct{}

var _ DeletionGuard = (*AlwaysAllowDeletion)(nil)

// NewAlwaysAllowDeletion builds the permissive guard.
func NewAlwaysAllowDeletion() *AlwaysAllowDeletion { return &AlwaysAllowDeletion{} }

// CanDelete always permits.
func (AlwaysAllowDeletion) CanDelete(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// ZoneVerifier reports whether a zone id refers to a real zone in a company.
//
// # Why this exists
//
// entity.Warehouse.assignZone validates that a zone id is well-formed and its
// kind is known. It cannot validate that the zone EXISTS, because zones belong
// to the future Location aggregate.
//
// Same reasoning as DeletionGuard: the cross-aggregate check is an interface
// this module declares, and the Location sprint implements it. Until then
// AcceptAnyZone is in force, and the referential guarantee on
// default_*_zone_id columns is therefore application-level rather than
// enforced by a foreign key. That gap is stated plainly in
// docs/Warehouse.md §7 rather than left for someone to discover.
type ZoneVerifier interface {
	// VerifyZone returns nil when the zone exists and belongs to the company.
	VerifyZone(ctx context.Context, companyID, zoneID uuid.UUID) error
}

// AcceptAnyZone accepts every well-formed zone id.
//
// In force until Location exists. Named rather than nil for the same reason as
// AlwaysAllowDeletion: an unwired verifier must not be indistinguishable from a
// permissive one.
type AcceptAnyZone struct{}

var _ ZoneVerifier = (*AcceptAnyZone)(nil)

// NewAcceptAnyZone builds the permissive verifier.
func NewAcceptAnyZone() *AcceptAnyZone { return &AcceptAnyZone{} }

// VerifyZone always accepts.
func (AcceptAnyZone) VerifyZone(context.Context, uuid.UUID, uuid.UUID) error { return nil }
