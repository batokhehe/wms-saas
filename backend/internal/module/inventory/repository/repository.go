// Package repository declares the persistence CONTRACT for the Inventory
// aggregate.
//
// This sprint delivers the interface ONLY — no GORM, no persistence model, no
// implementation. The concrete transactional repository is a later sprint's
// work and will live in this same package, translating between the aggregate
// (unexported fields) and a persistence model exactly as the warehouse, location
// and product repositories do.
//
// The signatures speak in *entity.Inventory and plain filters. Every method
// takes a companyID, because inventory is tenant-owned: RepositoryConvention §3
// makes the tenant a required argument so forgetting it does not compile.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ListFilter narrows an inventory listing.
//
// A plain struct rather than a DTO: the repository must not depend on a
// transport contract. Every field is optional; a zero value means "do not filter
// on this".
type ListFilter struct {
	Paging      pagination.Request
	WarehouseID uuid.UUID
	LocationID  uuid.UUID
	ProductID   uuid.UUID
	Tracking    string
	Status      string
}

// Repository is the persistence contract for the Inventory aggregate.
//
// The find methods encode how stock is addressed under each tracking type:
//
//   - FindByProductLocation returns EVERY record for a product in a location —
//     one for NONE, many for LOT/SERIAL — because that is the natural unit a
//     picking or availability screen asks for.
//   - FindByLot / FindBySerial resolve the single record a lot or serial names.
//
// No method returns a persistence model; the eventual implementation translates
// to the aggregate before anything leaves this boundary.
type Repository interface {
	// Save persists a new aggregate or an update to an existing one, advancing
	// the optimistic-lock version. The implementation is transactional.
	Save(ctx context.Context, inv *entity.Inventory) error

	// FindByID returns one record, or NOT_FOUND. A record belonging to another
	// company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
	FindByID(ctx context.Context, inventoryID, companyID uuid.UUID) (*entity.Inventory, error)

	// FindByProductLocation returns every inventory record for a product within a
	// location: one for NONE tracking, one per lot or serial otherwise.
	FindByProductLocation(ctx context.Context, companyID, productID, locationID uuid.UUID) ([]*entity.Inventory, error)

	// FindByLot resolves the single record a lot names within a product and
	// location, or NOT_FOUND.
	FindByLot(ctx context.Context, companyID, productID, locationID uuid.UUID, lot string) (*entity.Inventory, error)

	// FindBySerial resolves the single record a serial names within a product, or
	// NOT_FOUND. A serial is unique per company and product, independent of
	// location.
	FindBySerial(ctx context.Context, companyID, productID uuid.UUID, serial string) (*entity.Inventory, error)

	// List returns a page of the company's inventory, filtered and paginated.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Inventory], error)

	// Exists reports whether any inventory record exists for a product in a
	// location. It backs the InventoryExists specification.
	Exists(ctx context.Context, companyID, productID, locationID uuid.UUID) (bool, error)
}
