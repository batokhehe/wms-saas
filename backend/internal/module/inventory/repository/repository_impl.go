package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// positionRepository is the GORM-backed implementation of Repository.
//
// It composes the generic base over the PERSISTENCE model, never the aggregate,
// which deliberately does not satisfy entity.Identifiable. The base is held as an
// unexported field rather than embedded so its CRUD over positionModel is not
// promoted onto this type — a caller cannot bypass the aggregate.
type positionRepository struct {
	repo *base.Base[positionModel, *positionModel]
	ids  port.IDGenerator
}

var _ Repository = (*positionRepository)(nil)

// New builds the repository.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &positionRepository{
		repo: base.New[positionModel, *positionModel](db, ids, "inventory.repository"),
		ids:  ids,
	}
}

// forTenant is the scope every query applies. A named helper means a reviewer
// auditing for missing tenant filters looks for one identifier.
func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

// GetOrCreatePosition returns the position a key addresses, opening an empty one
// when absent.
//
// The new position is NOT written here. The caller books its movement through the
// aggregate and then Updates, so a failed operation never leaves an empty row
// behind — and the insert happens inside the caller's transaction, not beside it.
func (r *positionRepository) GetOrCreatePosition(
	ctx context.Context, key entity.StockKey, actorID uuid.UUID,
) (*entity.InventoryPosition, error) {
	existing, err := r.FindByKey(ctx, key)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, apperror.ErrNotFound):
		return nil, err
	}

	// GORM manages created_at/updated_at on write; the aggregate needs a stamp
	// now, and the repository is the layer that owns persistence timestamps.
	return entity.NewInventoryPosition(r.ids.NewID(), key, actorID, nowUTC())
}

// Update persists a position, inserting it when new and applying the optimistic
// lock when it already exists.
//
// A position created by GetOrCreatePosition has never been written, so its first
// Update is an INSERT; every later one is a conditional UPDATE that advances the
// version only when the stored token still matches.
func (r *positionRepository) Update(ctx context.Context, position *entity.InventoryPosition) error {
	model := toModel(position)

	exists, err := r.repo.Exists(ctx, model.ID, forTenant(model.CompanyID))
	if err != nil {
		return err
	}
	if !exists {
		return r.repo.Create(ctx, model)
	}
	return r.repo.UpdateOptimistic(ctx, model)
}

// FindByID returns one position, or NOT_FOUND.
func (r *positionRepository) FindByID(
	ctx context.Context, positionID, companyID uuid.UUID,
) (*entity.InventoryPosition, error) {
	model, err := r.repo.FindByID(ctx, positionID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// FindByKey resolves the position a StockKey addresses.
//
// The lot and serial predicates are NULL-aware: an untracked position stores NULL
// in both columns, and `lot_number = NULL` matches nothing in SQL, so the filter
// must be IS NULL instead.
func (r *positionRepository) FindByKey(
	ctx context.Context, key entity.StockKey,
) (*entity.InventoryPosition, error) {
	attrs := key.Attributes()

	scopes := []base.Scope{
		forTenant(key.CompanyID()),
		base.Where("warehouse_id = ?", key.WarehouseID()),
		base.Where("location_id = ?", key.LocationID()),
		base.Where("product_id = ?", key.ProductID()),
		base.Where("tracking_type = ?", attrs.Tracking().String()),
		nullableEquals("lot_number", attrs.Lot().String()),
		nullableEquals("serial_number", attrs.Serial().String()),
	}

	model, err := r.repo.FindOne(ctx, scopes...)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// List returns a page of the company's positions.
func (r *positionRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.InventoryPosition], error) {
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

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.InventoryPosition]{}, err
	}
	return pagination.Page[*entity.InventoryPosition]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

// nullableEquals matches a column against a value, or against NULL when the value
// is absent. Without the NULL branch an untracked position could never be found
// by key, because SQL equality against NULL is never true.
func nullableEquals(column, value string) base.Scope {
	if strings.TrimSpace(value) == "" {
		return base.Where(column + " IS NULL")
	}
	return base.Where(column+" = ?", strings.TrimSpace(value))
}
