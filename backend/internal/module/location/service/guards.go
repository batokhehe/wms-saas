// Package service orchestrates the StorageLocation aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.StorageLocation. The service loads an aggregate,
// gathers any cross-aggregate FACTS the rule needs, calls one domain method,
// persists and publishes.
//
// The test for whether a rule belongs here: can the aggregate answer it by
// looking only at itself? "May a LOCKED location receive stock?" — yes, that is
// the aggregate's. "How full is it right now?" — no, stock is another
// aggregate, so the service fetches the fact and hands it over.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// This file declares FIVE extension points. None is implemented here, and none
// will be implemented by this module.
//
// They share a shape: the location module declares the interface it needs, a
// future module implements it, and bootstrap injects the implementation. No
// file in this package changes when that happens — which is the property the
// tests assert by substituting blocking fakes.
//
// Every default is a NAMED type rather than nil. A nil guard would make "no
// guard configured" and "the guard permits it" indistinguishable, so a wiring
// mistake would silently disable a safety check.

// ---------- CurrentCapacityProvider ----------

// CurrentCapacityProvider reports how full a location is right now.
//
// # Why this exists
//
// The aggregate's central rule is "capacity may not be reduced below what is
// currently stored". The aggregate cannot evaluate that alone: stock is another
// aggregate, and one that loaded another would collapse the consistency
// boundary that makes each independent.
//
// So the service fetches the usage and passes it INTO
// StorageLocation.ChangeCapacity. The RULE stays in the domain; only the FACT
// comes from outside. That is what keeps the rule testable with no
// infrastructure — see the aggregate's tests, which supply usage directly.
//
// # What the Inventory sprint does
//
// It implements this — summing on-hand weight, volume and pallet positions for
// the location — and bootstrap injects it in place of EmptyCapacity.
type CurrentCapacityProvider interface {
	// CurrentUsage returns what is stored in a location.
	//
	// It takes a companyID as well as a location id so an implementation cannot
	// accidentally query across tenants — the same discipline the repositories
	// follow.
	CurrentUsage(ctx context.Context, companyID, locationID uuid.UUID) (entity.Usage, error)
}

// EmptyCapacity reports every location as empty.
//
// In force until Inventory exists. Reporting empty rather than refusing is
// correct for this default: with no stock module, no location CAN hold stock,
// so "nothing is stored here" is the truth rather than a permissive guess.
type EmptyCapacity struct{}

var _ CurrentCapacityProvider = (*EmptyCapacity)(nil)

// NewEmptyCapacity builds the default provider.
func NewEmptyCapacity() *EmptyCapacity { return &EmptyCapacity{} }

// CurrentUsage always reports empty.
func (EmptyCapacity) CurrentUsage(
	context.Context, uuid.UUID, uuid.UUID,
) (entity.Usage, error) {
	return entity.Usage{}, nil
}

// ---------- InventoryProvider ----------

// InventoryProvider answers questions about what a location holds.
//
// Distinct from CurrentCapacityProvider, which measures VOLUME. This one
// answers questions of KIND and PRESENCE: how many different SKUs are here, and
// is the location empty enough to retire?
//
// They are separate interfaces because they will have different implementations
// and different costs — a capacity sum is an aggregate query over stock rows,
// while "is this empty?" is an existence check that can stop at the first row.
type InventoryProvider interface {
	// DistinctSKUs reports how many different products occupy a location.
	//
	// A negative return means "unknown", which the aggregate treats as
	// permissive — see StorageLocation.DisableMixedSKU. Blocking every call
	// until Inventory exists would make the operation unusable for no safety
	// benefit.
	DistinctSKUs(ctx context.Context, companyID, locationID uuid.UUID) (int, error)

	// IsEmpty reports whether a location holds no stock at all. It backs the
	// archive check.
	IsEmpty(ctx context.Context, companyID, locationID uuid.UUID) (bool, error)
}

// EmptyInventory reports every location as holding nothing.
//
// In force until Inventory exists, and truthful for the same reason as
// EmptyCapacity: with no stock module, nothing is stored anywhere.
type EmptyInventory struct{}

var _ InventoryProvider = (*EmptyInventory)(nil)

// NewEmptyInventory builds the default provider.
func NewEmptyInventory() *EmptyInventory { return &EmptyInventory{} }

// DistinctSKUs always reports zero.
func (EmptyInventory) DistinctSKUs(context.Context, uuid.UUID, uuid.UUID) (int, error) {
	return 0, nil
}

// IsEmpty always reports true.
func (EmptyInventory) IsEmpty(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

// ---------- ReceivingGuard ----------

// ReceivingGuard reports whether stock may be put away into a location,
// considering aggregates OUTSIDE the location.
//
// # Why the aggregate is not enough
//
// StorageLocation.CanReceiveInventory answers the LOCAL half: a LOCKED,
// INACTIVE or MAINTENANCE location refuses. It cannot answer "is this product
// compatible with this location?" or "would this exceed capacity?", because
// products and stock are other aggregates.
//
// The service composes the two — ask the aggregate, then ask the guard — so
// neither half can be skipped and neither has to know about the other.
//
// # What implements it
//
// The Receiving sprint, with product-compatibility and capacity checks.
type ReceivingGuard interface {
	// CanReceive returns nil when putaway is permitted, or a typed error
	// explaining why not.
	CanReceive(ctx context.Context, companyID, locationID uuid.UUID) error
}

// AllowAllReceiving permits every putaway the aggregate already allows.
//
// It does NOT bypass the aggregate: CanReceiveInventory still refuses a locked
// location. This default only declines to add further restrictions.
type AllowAllReceiving struct{}

var _ ReceivingGuard = (*AllowAllReceiving)(nil)

// NewAllowAllReceiving builds the default guard.
func NewAllowAllReceiving() *AllowAllReceiving { return &AllowAllReceiving{} }

// CanReceive always permits.
func (AllowAllReceiving) CanReceive(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// ---------- PickingGuard ----------

// PickingGuard reports whether stock may be taken from a location.
//
// The mirror of ReceivingGuard, and deliberately a separate interface: the two
// have genuinely different rules. A MAINTENANCE location may still be picked
// FROM — work is scheduled on a rack precisely so its remaining stock can be
// drained first — while it may not be received INTO.
//
// Merging them into one "CanUse" would erase that distinction and either strand
// stock or allow putaway into a rack about to be dismantled.
//
// Implemented by the Picking sprint.
type PickingGuard interface {
	// CanPick returns nil when picking is permitted, or a typed error.
	CanPick(ctx context.Context, companyID, locationID uuid.UUID) error
}

// AllowAllPicking permits every pick the aggregate already allows.
type AllowAllPicking struct{}

var _ PickingGuard = (*AllowAllPicking)(nil)

// NewAllowAllPicking builds the default guard.
func NewAllowAllPicking() *AllowAllPicking { return &AllowAllPicking{} }

// CanPick always permits.
func (AllowAllPicking) CanPick(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------- CycleCountGuard ----------

// CycleCountGuard reports whether a location may be counted.
//
// Separate from the other two because counting has an inverted relationship to
// availability: a location is often LOCKED *in order to* count it, so a guard
// that reused the receiving rules would refuse exactly when a count is most
// needed.
//
// Implemented by the Cycle Count sprint.
type CycleCountGuard interface {
	// CanCount returns nil when a count may proceed, or a typed error.
	CanCount(ctx context.Context, companyID, locationID uuid.UUID) error
}

// AllowAllCycleCount permits every count.
type AllowAllCycleCount struct{}

var _ CycleCountGuard = (*AllowAllCycleCount)(nil)

// NewAllowAllCycleCount builds the default guard.
func NewAllowAllCycleCount() *AllowAllCycleCount { return &AllowAllCycleCount{} }

// CanCount always permits.
func (AllowAllCycleCount) CanCount(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------- WarehouseVerifier ----------

// WarehouseVerifier confirms a warehouse exists and belongs to a company.
//
// # Why this is not a repository call
//
// Warehouse is another aggregate in another module, and ModuleConvention §6
// forbids importing its repository. More fundamentally, a location aggregate
// that loaded a warehouse aggregate would collapse the boundary that lets each
// be modified independently.
//
// So the location module declares the narrow interface it needs — one method,
// returning only an error — and bootstrap adapts the warehouse module's
// repository to it. The warehouse module is not modified and does not learn
// that locations exist.
//
// This one IS wired in production, unlike the four above: warehouses exist
// today, so there is a real implementation to inject.
type WarehouseVerifier interface {
	// VerifyWarehouse returns nil when the warehouse exists in the company and
	// may have locations defined in it.
	VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AcceptAnyWarehouse accepts every well-formed warehouse id.
//
// Used only in tests. Production wires the real adapter — see
// bootstrap/warehouse_directory.go — because accepting an unknown warehouse
// would let a caller create locations in a site that does not exist, which the
// foreign key would reject with an opaque 500 rather than a clear message.
type AcceptAnyWarehouse struct{}

var _ WarehouseVerifier = (*AcceptAnyWarehouse)(nil)

// NewAcceptAnyWarehouse builds the permissive verifier.
func NewAcceptAnyWarehouse() *AcceptAnyWarehouse { return &AcceptAnyWarehouse{} }

// VerifyWarehouse always accepts.
func (AcceptAnyWarehouse) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// ErrWarehouseNotFound is the error a WarehouseVerifier should return when a
// warehouse is unknown or belongs to another tenant.
//
// Exported so the bootstrap adapter can return exactly what the service
// expects, rather than each implementation inventing its own wording — and
// NOT_FOUND rather than FORBIDDEN, because distinguishing them would confirm
// that a warehouse with that id exists in some other company.
func ErrWarehouseNotFound() error {
	return apperror.NotFound("The requested warehouse was not found").
		WithOp("location.WarehouseVerifier")
}
