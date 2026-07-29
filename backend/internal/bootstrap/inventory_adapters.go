package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	inventoryservice "github.com/batokhehe/wms-saas/backend/internal/module/inventory/service"
	locationrepo "github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	productrepo "github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	warehouserepo "github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// This file joins the inventory module to warehouse, location and product,
// without inventory importing any of them.
//
// Inventory declares the narrow provider interfaces it needs (../inventory/port);
// bootstrap — the composition root, the one place permitted to know every module
// — supplies implementations over the other modules' repositories. Only the
// ANSWER crosses each boundary (an error or nil), never a foreign aggregate, so
// inventory never sees another module's internals and no aggregate loads another.
//
// The alternative — a direct cross-module import — would break ModuleConvention
// §6 and let one aggregate load another, collapsing the consistency boundary.

// ---------- inventory → product ----------

type inventoryProductProvider struct {
	products productrepo.Repository
}

var _ inventoryservice.ProductVerifier = (*inventoryProductProvider)(nil)

func newInventoryProductProvider(products productrepo.Repository) *inventoryProductProvider {
	return &inventoryProductProvider{products: products}
}

// VerifyProduct confirms a product exists in the company. "Inventory cannot exist
// without Product" is enforced here, at creation time.
func (p *inventoryProductProvider) VerifyProduct(ctx context.Context, companyID, productID uuid.UUID) error {
	if _, err := p.products.FindByID(ctx, productID, companyID); err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.NewValidation(apperror.FieldError{
				Field: "product_id", Rule: "not_found",
				Message: "no product with this id exists in this company",
			}).WithOp("bootstrap.inventoryProductProvider.VerifyProduct")
		}
		return err
	}
	return nil
}

// ---------- inventory → warehouse ----------

type inventoryWarehouseProvider struct {
	warehouses warehouserepo.Repository
}

var _ inventoryservice.WarehouseVerifier = (*inventoryWarehouseProvider)(nil)

func newInventoryWarehouseProvider(warehouses warehouserepo.Repository) *inventoryWarehouseProvider {
	return &inventoryWarehouseProvider{warehouses: warehouses}
}

// VerifyWarehouse confirms a warehouse exists in the company.
func (p *inventoryWarehouseProvider) VerifyWarehouse(ctx context.Context, companyID, warehouseID uuid.UUID) error {
	if _, err := p.warehouses.FindByID(ctx, warehouseID, companyID); err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.NewValidation(apperror.FieldError{
				Field: "warehouse_id", Rule: "not_found",
				Message: "no warehouse with this id exists in this company",
			}).WithOp("bootstrap.inventoryWarehouseProvider.VerifyWarehouse")
		}
		return err
	}
	return nil
}

// ---------- inventory → location ----------

type inventoryLocationProvider struct {
	locations locationrepo.Repository
}

var _ inventoryservice.LocationVerifier = (*inventoryLocationProvider)(nil)

func newInventoryLocationProvider(locations locationrepo.Repository) *inventoryLocationProvider {
	return &inventoryLocationProvider{locations: locations}
}

// VerifyLocation confirms a location exists in the company AND belongs to the
// given warehouse — the "Location belongs to Warehouse" invariant. It also
// refuses an archived location: stock must not be booked into a bin removed from
// the layout.
func (p *inventoryLocationProvider) VerifyLocation(ctx context.Context, companyID, warehouseID, locationID uuid.UUID) error {
	location, err := p.locations.FindByID(ctx, locationID, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.NewValidation(apperror.FieldError{
				Field: "location_id", Rule: "not_found",
				Message: "no storage location with this id exists in this company",
			}).WithOp("bootstrap.inventoryLocationProvider.VerifyLocation")
		}
		return err
	}
	if location.WarehouseID() != warehouseID {
		return apperror.NewValidation(apperror.FieldError{
			Field: "location_id", Rule: "mismatch",
			Message: "this storage location does not belong to the given warehouse",
		}).WithOp("bootstrap.inventoryLocationProvider.VerifyLocation")
	}
	if location.IsArchived() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "location_id", Rule: "archived",
			Message: "this storage location has been archived and cannot hold stock",
		}).WithOp("bootstrap.inventoryLocationProvider.VerifyLocation")
	}
	return nil
}
