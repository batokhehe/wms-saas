package service

import (
	"context"

	"github.com/google/uuid"
)

// This file declares the purchase-order module's EXTERNAL VERIFIERS: the narrow
// interfaces the application service needs another module to satisfy so it can
// enforce the cross-aggregate rules the PurchaseOrder aggregate cannot check
// alone.
//
// The service NEVER reaches into another module's repository. It declares what it
// needs here; bootstrap — the composition root — supplies an implementation over
// the other module's repository. Only the ANSWER crosses the boundary (an error,
// or nil), never a foreign aggregate.
//
// Every default below is a NAMED permissive type, never nil, so an unwired
// verifier cannot be mistaken for "the verifier permits it".

// SupplierVerifier confirms a supplier exists in a company and may be ordered
// from. Ordering from a supplier that does not exist — or one that has been
// deactivated — is exactly what deactivation is meant to prevent.
type SupplierVerifier interface {
	VerifySupplier(ctx context.Context, companyID, supplierID uuid.UUID) error
}

// AcceptAnySupplier accepts every well-formed supplier id.
type AcceptAnySupplier struct{}

var _ SupplierVerifier = (*AcceptAnySupplier)(nil)

// NewAcceptAnySupplier builds the permissive verifier.
func NewAcceptAnySupplier() *AcceptAnySupplier { return &AcceptAnySupplier{} }

// VerifySupplier always accepts.
func (AcceptAnySupplier) VerifySupplier(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// WarehouseVerifier confirms the destination warehouse exists in the company.
type WarehouseVerifier interface {
	VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AcceptAnyWarehouse accepts every well-formed warehouse id.
type AcceptAnyWarehouse struct{}

var _ WarehouseVerifier = (*AcceptAnyWarehouse)(nil)

// NewAcceptAnyWarehouse builds the permissive verifier.
func NewAcceptAnyWarehouse() *AcceptAnyWarehouse { return &AcceptAnyWarehouse{} }

// VerifyWarehouse always accepts.
func (AcceptAnyWarehouse) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ProductVerifier confirms an ordered product exists in the company.
type ProductVerifier interface {
	VerifyProduct(ctx context.Context, companyID, productID uuid.UUID) error
}

// AcceptAnyProduct accepts every well-formed product id.
type AcceptAnyProduct struct{}

var _ ProductVerifier = (*AcceptAnyProduct)(nil)

// NewAcceptAnyProduct builds the permissive verifier.
func NewAcceptAnyProduct() *AcceptAnyProduct { return &AcceptAnyProduct{} }

// VerifyProduct always accepts.
func (AcceptAnyProduct) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error { return nil }
