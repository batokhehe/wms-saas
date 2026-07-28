// Package specification holds the Inventory module's business SPECIFICATIONS —
// named rules that an aggregate cannot evaluate because they concern a SET of
// records, not a single one.
//
// It follows the convention product established (its specifications live in the
// service package; this sprint has no service, so they live here). Each rule is
// a small object: the set-level ones wrap the repository contract, the pure one
// is a predicate over a single aggregate. Each returns a typed error when
// violated so a caller gets a clear, classified failure rather than a bare bool.
package specification

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// InventoryExists reports whether a stock position already exists for a product
// in a location. It backs both "confirm there is stock to operate on" and
// "refuse a duplicate NONE-tracked record".
type InventoryExists struct {
	inventories repository.Repository
}

// NewInventoryExists builds the specification.
func NewInventoryExists(inventories repository.Repository) InventoryExists {
	return InventoryExists{inventories: inventories}
}

// Holds reports whether any inventory record exists for the product in the
// location.
func (s InventoryExists) Holds(ctx context.Context, companyID, productID, locationID uuid.UUID) (bool, error) {
	return s.inventories.Exists(ctx, companyID, productID, locationID)
}

// EnoughAvailableInventory is a PURE specification over a single aggregate:
// given a record and a requested amount, is there enough available to satisfy
// it? It needs no repository, so it is reusable both before a reservation and in
// a test with no infrastructure.
type EnoughAvailableInventory struct{}

// NewEnoughAvailableInventory builds the specification.
func NewEnoughAvailableInventory() EnoughAvailableInventory { return EnoughAvailableInventory{} }

// Holds reports whether the record has at least the requested amount available.
func (EnoughAvailableInventory) Holds(inv *entity.Inventory, requested entity.Quantity) bool {
	if inv == nil {
		return false
	}
	return inv.Available().Value() >= requested.Value()
}

// Ensure returns a CONFLICT when the record cannot satisfy the requested amount.
func (s EnoughAvailableInventory) Ensure(inv *entity.Inventory, requested entity.Quantity) error {
	if !s.Holds(inv, requested) {
		return apperror.Conflict("insufficient available inventory").
			WithOp("inventory.spec.EnoughAvailableInventory")
	}
	return nil
}

// UniqueSerial enforces "a serial identifies at most one record per company and
// product". A serial number is a globally unique physical unit; two records
// claiming it would make a scan ambiguous.
type UniqueSerial struct {
	inventories repository.Repository
}

// NewUniqueSerial builds the specification.
func NewUniqueSerial(inventories repository.Repository) UniqueSerial {
	return UniqueSerial{inventories: inventories}
}

// Ensure returns a CONFLICT when the serial already exists for the product.
func (s UniqueSerial) Ensure(ctx context.Context, companyID, productID uuid.UUID, serial string) error {
	_, err := s.inventories.FindBySerial(ctx, companyID, productID, serial)
	switch {
	case err == nil:
		return apperror.Conflict("a record with this serial number already exists").
			WithOp("inventory.spec.UniqueSerial")
	case errors.Is(err, apperror.ErrNotFound):
		return nil
	default:
		return err
	}
}

// UniqueLot enforces "a lot identifies at most one record per product and
// location". Two records for the same lot in the same bin would split one batch
// into two conflicting counts.
type UniqueLot struct {
	inventories repository.Repository
}

// NewUniqueLot builds the specification.
func NewUniqueLot(inventories repository.Repository) UniqueLot {
	return UniqueLot{inventories: inventories}
}

// Ensure returns a CONFLICT when the lot already exists for the product in the
// location.
func (s UniqueLot) Ensure(ctx context.Context, companyID, productID, locationID uuid.UUID, lot string) error {
	_, err := s.inventories.FindByLot(ctx, companyID, productID, locationID, lot)
	switch {
	case err == nil:
		return apperror.Conflict("a record with this lot number already exists in this location").
			WithOp("inventory.spec.UniqueLot")
	case errors.Is(err, apperror.ErrNotFound):
		return nil
	default:
		return err
	}
}
