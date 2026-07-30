package service

import (
	"context"

	"github.com/google/uuid"

	ledgerdto "github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
)

// This file declares the inventory module's EXTERNAL VERIFIERS: the narrow
// interfaces the application service needs another module to satisfy so it can
// enforce the cross-aggregate rules the Inventory aggregate cannot check alone.
//
// The service NEVER reaches into another module's repository. It declares what
// it needs here; bootstrap — the composition root — supplies an implementation
// over the other module's repository. Only the ANSWER crosses the boundary (an
// error, or a policy value), never a foreign aggregate.
//
// ProductVerifier, WarehouseVerifier and LocationVerifier are structurally
// identical to the provider interfaces in ../port, which the bootstrap adapters
// already implement. Go interfaces are structural, so the SAME adapter satisfies
// both — these names exist because the application service is the consumer and
// names its own contracts, and no wiring changes were needed to adopt them.
//
// Every default below is a NAMED permissive type, never nil, so an unwired
// verifier cannot be mistaken for "the verifier permits it".

// ---------------------------------------------------------------------------
// ProductVerifier
// ---------------------------------------------------------------------------

// ProductVerifier confirms a product exists in a company. Stock for a product
// that does not exist is meaningless, and the aggregate cannot load the Product
// aggregate to check.
type ProductVerifier interface {
	// VerifyProduct returns nil when the product exists in the company.
	VerifyProduct(ctx context.Context, companyID, productID uuid.UUID) error
}

// AcceptAnyProduct accepts every well-formed product id.
type AcceptAnyProduct struct{}

var _ ProductVerifier = (*AcceptAnyProduct)(nil)

// NewAcceptAnyProduct builds the permissive verifier.
func NewAcceptAnyProduct() *AcceptAnyProduct { return &AcceptAnyProduct{} }

// VerifyProduct always accepts.
func (AcceptAnyProduct) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// WarehouseVerifier
// ---------------------------------------------------------------------------

// WarehouseVerifier confirms a warehouse exists in a company — the "warehouse
// belongs to company" rule, which references the Warehouse aggregate.
type WarehouseVerifier interface {
	// VerifyWarehouse returns nil when the warehouse exists in the company.
	VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AcceptAnyWarehouse accepts every well-formed warehouse id.
type AcceptAnyWarehouse struct{}

var _ WarehouseVerifier = (*AcceptAnyWarehouse)(nil)

// NewAcceptAnyWarehouse builds the permissive verifier.
func NewAcceptAnyWarehouse() *AcceptAnyWarehouse { return &AcceptAnyWarehouse{} }

// VerifyWarehouse always accepts.
func (AcceptAnyWarehouse) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// LocationVerifier
// ---------------------------------------------------------------------------

// LocationVerifier confirms a location exists in a company AND belongs to the
// given warehouse. It takes the warehouse id as well as the location id
// precisely so that relationship can be checked, not merely existence.
type LocationVerifier interface {
	// VerifyLocation returns nil when the location exists in the company and
	// belongs to the warehouse.
	VerifyLocation(ctx context.Context, companyID, warehouseID, locationID uuid.UUID) error
}

// AcceptAnyLocation accepts every well-formed location id.
type AcceptAnyLocation struct{}

var _ LocationVerifier = (*AcceptAnyLocation)(nil)

// NewAcceptAnyLocation builds the permissive verifier.
func NewAcceptAnyLocation() *AcceptAnyLocation { return &AcceptAnyLocation{} }

// VerifyLocation always accepts.
func (AcceptAnyLocation) VerifyLocation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

// ---------------------------------------------------------------------------
// StockPolicyProvider
// ---------------------------------------------------------------------------

// StockPolicyProvider supplies the per-company stock POLICY the service applies
// before delegating to the aggregate.
//
// # Why a policy seam rather than aggregate rules
//
// The Inventory aggregate enforces what is universally true of a stock position
// (on-hand never negative, on-hand never below reserved, a serial is one unit).
// A POLICY is different: it is a per-tenant configuration choice that a company
// can change without the domain model changing. "May this company receive stock
// into a location that already holds a different lot?" is policy; "may on-hand
// go negative?" is an invariant.
//
// Keeping policy behind this interface means the aggregate stays free of
// configuration, and the Configuration/Policy sprint implements it without
// touching a single inventory file.
type StockPolicyProvider interface {
	// AllowNegativeStock reports whether the company permits an issue that would
	// drive a position below zero. The aggregate refuses it regardless today —
	// this is the seam through which a future backorder policy is expressed.
	AllowNegativeStock(ctx context.Context, companyID uuid.UUID) (bool, error)

	// RequireQuarantineOnReceipt reports whether received stock must be placed
	// under a quarantine hold before it becomes available.
	RequireQuarantineOnReceipt(ctx context.Context, companyID, productID uuid.UUID) (bool, error)
}

// DefaultStockPolicy is the conservative policy in force until a policy module
// exists: no negative stock, no automatic quarantine.
//
// Both answers are the SAFE default rather than a permissive guess — allowing
// negative stock or skipping quarantine by default would silently weaken a
// control that a company never opted out of.
type DefaultStockPolicy struct{}

var _ StockPolicyProvider = (*DefaultStockPolicy)(nil)

// NewDefaultStockPolicy builds the default policy.
func NewDefaultStockPolicy() *DefaultStockPolicy { return &DefaultStockPolicy{} }

// AllowNegativeStock always reports false.
func (DefaultStockPolicy) AllowNegativeStock(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}

// RequireQuarantineOnReceipt always reports false.
func (DefaultStockPolicy) RequireQuarantineOnReceipt(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// LedgerPublisher
// ---------------------------------------------------------------------------

// LedgerPublisher records stock movements in the append-only inventory ledger.
type LedgerPublisher interface {
	RecordMovement(ctx context.Context, movement ledgerdto.RecordMovementRequest) error
}

// NoopLedgerPublisher is the permissive default when no ledger service is wired.
type NoopLedgerPublisher struct{}

var _ LedgerPublisher = (*NoopLedgerPublisher)(nil)

// NewNoopLedgerPublisher builds the default no-op ledger publisher.
func NewNoopLedgerPublisher() *NoopLedgerPublisher { return &NoopLedgerPublisher{} }

// RecordMovement always returns nil without recording.
func (NoopLedgerPublisher) RecordMovement(context.Context, ledgerdto.RecordMovementRequest) error {
	return nil
}
