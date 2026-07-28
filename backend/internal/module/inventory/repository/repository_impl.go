package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
)

// inventoryRepository is the GORM-backed implementation of Repository.
//
// It composes the generic base over the PERSISTENCE model — never the aggregate,
// which deliberately does not satisfy entity.Identifiable. The base is held as
// an unexported FIELD rather than embedded, so its CRUD over inventoryModel is
// not promoted onto this type; a caller cannot bypass the aggregate.
type inventoryRepository struct {
	repo *base.Base[inventoryModel, *inventoryModel]
	tx   transaction.Manager
}

var _ Repository = (*inventoryRepository)(nil)

// New builds the repository.
//
// It holds a transaction.Manager of its own so Save can decide create-vs-update
// and write atomically even when a caller invokes it outside a service
// transaction. When a caller is already in one, the manager joins it via a
// SAVEPOINT rather than opening a second — so the write is one unit either way.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &inventoryRepository{
		repo: base.New[inventoryModel, *inventoryModel](db, ids, "inventory.repository"),
		tx:   transaction.NewGormManager(db),
	}
}

// forTenant is the scope every method applies. A named helper means a reviewer
// auditing for missing tenant filters looks for one identifier, not an inline
// Where clause easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

// Save persists a new aggregate or an update to an existing one, atomically.
//
// A single Save covers both because the aggregate's Repository interface exposes
// one write method. Which path runs is decided by whether a row already exists:
// a create for a fresh aggregate, an optimistic update for a loaded one. The
// decision and the write share one transaction, so two concurrent creates of the
// same id cannot both insert (the primary key stops the second), and an update
// races only through the version check below.
func (r *inventoryRepository) Save(ctx context.Context, inv *entity.Inventory) error {
	return r.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		model := toModel(inv)

		exists, err := r.repo.Exists(ctx, model.ID, forTenant(model.CompanyID))
		if err != nil {
			return err
		}
		if exists {
			// UpdateOptimistic advances the version through
			// WHERE id = ? AND version = ?, returning CONFLICT
			// (ErrConcurrentModification) when another writer already moved it.
			return r.repo.UpdateOptimistic(ctx, model)
		}
		return r.repo.Create(ctx, model)
	})
}

// FindByID returns one record, or NOT_FOUND. A record in another company is
// NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
func (r *inventoryRepository) FindByID(
	ctx context.Context, inventoryID, companyID uuid.UUID,
) (*entity.Inventory, error) {
	model, err := r.repo.FindByID(ctx, inventoryID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// FindByProductLocation returns every inventory record for a product in a
// location — one row for NONE, many for LOT/SERIAL.
func (r *inventoryRepository) FindByProductLocation(
	ctx context.Context, companyID, productID, locationID uuid.UUID,
) ([]*entity.Inventory, error) {
	models, err := r.repo.FindMany(ctx,
		forTenant(companyID),
		base.Where("product_id = ?", productID),
		base.Where("location_id = ?", locationID),
	)
	if err != nil {
		return nil, err
	}
	return toDomainSlice(models), nil
}

// FindByLot resolves the single record a lot names within a product and
// location, or NOT_FOUND.
func (r *inventoryRepository) FindByLot(
	ctx context.Context, companyID, productID, locationID uuid.UUID, lot string,
) (*entity.Inventory, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("product_id = ?", productID),
		base.Where("location_id = ?", locationID),
		base.Where("lot_number = ?", normalize(lot)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// FindBySerial resolves the single record a serial names within a product, or
// NOT_FOUND. Serials are globally unique, but the lookup is still company-scoped
// so one tenant cannot resolve another's serial.
func (r *inventoryRepository) FindBySerial(
	ctx context.Context, companyID, productID uuid.UUID, serial string,
) (*entity.Inventory, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("product_id = ?", productID),
		base.Where("serial_number = ?", normalize(serial)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// List returns a page of the company's inventory, filtered and paginated.
func (r *inventoryRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.Inventory], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if filter.WarehouseID != uuid.Nil {
		scopes = append(scopes, base.Where("warehouse_id = ?", filter.WarehouseID))
	}
	if filter.LocationID != uuid.Nil {
		scopes = append(scopes, base.Where("location_id = ?", filter.LocationID))
	}
	if filter.ProductID != uuid.Nil {
		scopes = append(scopes, base.Where("product_id = ?", filter.ProductID))
	}
	if filter.Tracking != "" {
		scopes = append(scopes, base.Where("tracking_type = ?", filter.Tracking))
	}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}

	// The base rejects a pagination.Request that has not been through Apply, so
	// an unvalidated sort column can never reach the SQL.
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Inventory]{}, err
	}

	return pagination.Page[*entity.Inventory]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

// Exists reports whether any inventory record exists for a product in a
// location. It backs the InventoryExists specification.
func (r *inventoryRepository) Exists(
	ctx context.Context, companyID, productID, locationID uuid.UUID,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("product_id = ?", productID),
		base.Where("location_id = ?", locationID),
	)
}

// normalize canonicalises a lot or serial for comparison. The columns are CITEXT
// so comparison is already case-insensitive; trimming keeps the query value
// consistent with what the value objects store, so a lookup and an insert cannot
// disagree.
func normalize(raw string) string { return strings.TrimSpace(raw) }
