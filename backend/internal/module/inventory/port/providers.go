// Package port declares the Inventory module's EXTENSION POINTS — the narrow
// interfaces it needs a future module to satisfy so it can enforce the
// cross-aggregate invariants it cannot check alone.
//
// This follows the exact convention established by warehouse, location and
// product: the module declares the interface it needs, a future module
// implements it, and the composition root injects the implementation. The only
// difference is location — warehouse/location/product declared these in their
// service package, and this sprint has no service, so they live here in a
// dedicated port package rather than being invented as a new pattern.
//
// Every default is a NAMED permissive type, never nil, so a wiring mistake
// (an unset provider) cannot be mistaken for "the provider permits it" and
// silently disable a referential check.
package port

import (
	"context"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// ProductProvider
// ---------------------------------------------------------------------------

// ProductProvider confirms a product exists in a company and yields the facts
// inventory needs about it. "Inventory cannot exist without Product" is enforced
// here at creation time: a stock position for a product that does not exist is
// meaningless, and the aggregate cannot load the Product aggregate to check.
//
// Implemented by an adapter over the Product module's repository (in bootstrap),
// exactly as location adapts warehouse. Until wired, AcceptAnyProduct is in
// force and the product reference is application-level rather than a foreign key.
type ProductProvider interface {
	// VerifyProduct returns nil when the product exists in the company.
	VerifyProduct(ctx context.Context, companyID, productID uuid.UUID) error
}

// AcceptAnyProduct accepts every well-formed product id.
type AcceptAnyProduct struct{}

var _ ProductProvider = (*AcceptAnyProduct)(nil)

// NewAcceptAnyProduct builds the permissive provider.
func NewAcceptAnyProduct() *AcceptAnyProduct { return &AcceptAnyProduct{} }

// VerifyProduct always accepts.
func (AcceptAnyProduct) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// WarehouseProvider
// ---------------------------------------------------------------------------

// WarehouseProvider confirms a warehouse exists in a company — the "Warehouse
// belongs to Company" invariant, which references the Warehouse aggregate.
type WarehouseProvider interface {
	// VerifyWarehouse returns nil when the warehouse exists in the company.
	VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error
}

// AcceptAnyWarehouse accepts every well-formed warehouse id.
type AcceptAnyWarehouse struct{}

var _ WarehouseProvider = (*AcceptAnyWarehouse)(nil)

// NewAcceptAnyWarehouse builds the permissive provider.
func NewAcceptAnyWarehouse() *AcceptAnyWarehouse { return &AcceptAnyWarehouse{} }

// VerifyWarehouse always accepts.
func (AcceptAnyWarehouse) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// LocationProvider
// ---------------------------------------------------------------------------

// LocationProvider confirms a location exists in a company AND belongs to the
// given warehouse — the "Location belongs to Warehouse" invariant. It takes the
// warehouse id as well as the location id precisely so that relationship can be
// checked, not merely the location's existence.
type LocationProvider interface {
	// VerifyLocation returns nil when the location exists in the company and
	// belongs to the warehouse.
	VerifyLocation(ctx context.Context, companyID, warehouseID, locationID uuid.UUID) error
}

// AcceptAnyLocation accepts every well-formed location id.
type AcceptAnyLocation struct{}

var _ LocationProvider = (*AcceptAnyLocation)(nil)

// NewAcceptAnyLocation builds the permissive provider.
func NewAcceptAnyLocation() *AcceptAnyLocation { return &AcceptAnyLocation{} }

// VerifyLocation always accepts.
func (AcceptAnyLocation) VerifyLocation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

// ---------------------------------------------------------------------------
// ReservationProvider
// ---------------------------------------------------------------------------

// ReservationProvider answers whether an outstanding reservation exists against
// an inventory record, considering the future Reservation aggregate.
//
// The inventory aggregate owns its own reserved COUNT, but the individual
// reservations (which order, which quantity, held until when) belong to another
// aggregate. A guard that must not release or lock stock while named
// reservations are live consults this. Until the Reservation sprint exists,
// NoReservations reports false — truthfully, because no reservation aggregate
// can hold anything yet.
type ReservationProvider interface {
	// HasActiveReservations reports whether any live reservation references the
	// inventory record.
	HasActiveReservations(ctx context.Context, companyID, inventoryID uuid.UUID) (bool, error)
}

// NoReservations reports that nothing is reserved externally.
type NoReservations struct{}

var _ ReservationProvider = (*NoReservations)(nil)

// NewNoReservations builds the default provider.
func NewNoReservations() *NoReservations { return &NoReservations{} }

// HasActiveReservations always reports false.
func (NoReservations) HasActiveReservations(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
