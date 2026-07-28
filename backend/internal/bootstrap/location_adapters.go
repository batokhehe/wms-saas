package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	locationrepo "github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	locationsvc "github.com/batokhehe/wms-saas/backend/internal/module/location/service"
	warehouserepo "github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	warehousesvc "github.com/batokhehe/wms-saas/backend/internal/module/warehouse/service"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// This file joins the warehouse and location modules in BOTH directions,
// without either importing the other.
//
// Each adapter lives here because bootstrap is the composition root — the one
// place permitted to know about every module, and whose entire job is joining
// them. Each module declares the narrow interface it needs; this file supplies
// an implementation over the other module's repository.
//
// The alternative — a direct cross-module import — would break
// ModuleConvention §6 and, more importantly, would let one aggregate load
// another, collapsing the consistency boundary that makes each independent.

// ---------- location → warehouse ----------

// warehouseVerifier adapts the warehouse repository to the location module's
// service.WarehouseVerifier.
//
// The location module needs to know that a warehouse exists in the company
// before defining locations in it. Only the ANSWER crosses the boundary — the
// adapter returns an error or nil, never a *entity.Warehouse — so the location
// module never sees warehouse internals.
type warehouseVerifier struct {
	warehouses warehouserepo.Repository
}

var _ locationsvc.WarehouseVerifier = (*warehouseVerifier)(nil)

func newWarehouseVerifier(warehouses warehouserepo.Repository) *warehouseVerifier {
	return &warehouseVerifier{warehouses: warehouses}
}

// VerifyWarehouse confirms a warehouse exists in the company.
//
// It deliberately does NOT require the warehouse to be ACTIVE. Locations are
// defined during commissioning, before the site is activated — indeed a
// warehouse cannot be activated until it has a zone, which is a location. A
// status check here would make that ordering impossible.
func (v *warehouseVerifier) VerifyWarehouse(
	ctx context.Context, companyID, warehouseID uuid.UUID,
) error {
	_, err := v.warehouses.FindByID(ctx, warehouseID, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			// The location module's own sentinel, so the message is consistent
			// whichever implementation is wired — and NOT_FOUND rather than
			// FORBIDDEN, because distinguishing them would confirm that a
			// warehouse with that id exists in some other company.
			return locationsvc.ErrWarehouseNotFound()
		}
		return err
	}

	return nil
}

// ---------- warehouse → location ----------

// zoneVerifier adapts the location repository to the warehouse module's
// service.ZoneVerifier.
//
// # What this closes
//
// The warehouse sprint declared ZoneVerifier for its default receiving,
// shipping and staging zone ids, and shipped AcceptAnyZone because no zone
// concept existed. docs/Warehouse.md §7 recorded that gap explicitly: "the
// referential guarantee is application-level today".
//
// Storage locations ARE those zones — a receiving dock is a location — so this
// adapter makes the guarantee real. Assigning a warehouse a zone id that is not
// a location in the same company now fails with a clear error instead of being
// silently accepted.
//
// The warehouse module is NOT modified: it declared the interface, and this is
// an implementation of it. That is the extension point working exactly as the
// previous sprint designed it.
type zoneVerifier struct {
	locations locationrepo.Repository
}

var _ warehousesvc.ZoneVerifier = (*zoneVerifier)(nil)

func newZoneVerifier(locations locationrepo.Repository) *zoneVerifier {
	return &zoneVerifier{locations: locations}
}

// VerifyZone confirms a zone id refers to a location in the company.
//
// It checks tenancy but not the warehouse, and that is a deliberate limit worth
// naming: the warehouse module's interface passes only a company and a zone id,
// so this adapter cannot tell whether the location belongs to the warehouse
// being configured. A company could therefore point warehouse A's receiving
// zone at a location in warehouse B.
//
// Widening the interface would mean editing the warehouse module, which this
// sprint must not do. The narrower check is still a large improvement over
// accepting any UUID, and the remaining gap is recorded in
// docs/StorageLocation.md §7.
func (v *zoneVerifier) VerifyZone(
	ctx context.Context, companyID, zoneID uuid.UUID,
) error {
	location, err := v.locations.FindByID(ctx, zoneID, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.NewValidation(apperror.FieldError{
				Field:   "zone_id",
				Rule:    "not_found",
				Message: "no storage location with this id exists in this company",
			}).WithOp("bootstrap.zoneVerifier.VerifyZone")
		}
		return err
	}

	// An archived location cannot serve as a warehouse's default zone: goods
	// would be routed to a place that has been removed from the layout.
	if location.IsArchived() {
		return apperror.NewValidation(apperror.FieldError{
			Field:   "zone_id",
			Rule:    "archived",
			Message: "this storage location has been archived and cannot be a default zone",
		}).WithOp("bootstrap.zoneVerifier.VerifyZone")
	}

	return nil
}
