package repository

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ledgerRepository is the GORM-backed implementation.
//
// The generic base is held as an unexported FIELD rather than embedded, and that
// matters more here than anywhere else in the system: embedding would promote the
// base's Update, UpdateFields, Delete and HardDelete onto this type, handing
// every caller the ability to rewrite the ledger. Composition keeps the write
// surface to exactly one method.
type ledgerRepository struct {
	repo *base.Base[ledgerEntryModel, *ledgerEntryModel]
}

var _ Repository = (*ledgerRepository)(nil)

// New builds the repository.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &ledgerRepository{
		repo: base.New[ledgerEntryModel, *ledgerEntryModel](db, ids, "inventoryledger.repository"),
	}
}

// forTenant is the scope every query applies.
func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

// Append writes a new entry — the ledger's only write path.
//
// It uses Create, never Save: Save would UPDATE a row whose primary key already
// exists, which is precisely the rewrite this module forbids. A duplicate id
// therefore surfaces as a CONFLICT from the primary key rather than silently
// replacing history.
func (r *ledgerRepository) Append(ctx context.Context, entry *entity.InventoryLedgerEntry) error {
	return r.repo.Create(ctx, toModel(entry))
}

// FindByID returns one entry, or NOT_FOUND.
func (r *ledgerRepository) FindByID(
	ctx context.Context, ledgerID, companyID uuid.UUID,
) (*entity.InventoryLedgerEntry, error) {
	model, err := r.repo.FindByID(ctx, ledgerID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

// FindByPosition returns one position's movement history.
func (r *ledgerRepository) FindByPosition(
	ctx context.Context, companyID, positionID uuid.UUID, paging pagination.Request,
) (pagination.Page[*entity.InventoryLedgerEntry], error) {
	page, err := r.repo.FindAll(ctx, paging,
		forTenant(companyID),
		base.Where("position_id = ?", positionID),
	)
	if err != nil {
		return pagination.Page[*entity.InventoryLedgerEntry]{}, err
	}
	return pagination.Page[*entity.InventoryLedgerEntry]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

// FindByReference returns every entry caused by one business document.
//
// It is unpaginated on purpose: the result is bounded by the size of a single
// document — the lines on one receipt — not by anything a tenant can grow without
// limit, which is the condition FindMany is reserved for.
func (r *ledgerRepository) FindByReference(
	ctx context.Context, companyID uuid.UUID, referenceType string, referenceID uuid.UUID,
) ([]*entity.InventoryLedgerEntry, error) {
	scopes := []base.Scope{
		forTenant(companyID),
		base.Where("reference_id = ?", referenceID),
	}
	if referenceType != "" {
		scopes = append(scopes, base.Where("reference_type = ?", referenceType))
	}

	models, err := r.repo.FindMany(ctx, scopes...)
	if err != nil {
		return nil, err
	}
	return toDomainSlice(models), nil
}

// List returns a filtered, paginated page of the company's entries.
func (r *ledgerRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.InventoryLedgerEntry], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if filter.PositionID != uuid.Nil {
		scopes = append(scopes, base.Where("position_id = ?", filter.PositionID))
	}
	if filter.ProductID != uuid.Nil {
		scopes = append(scopes, base.Where("product_id = ?", filter.ProductID))
	}
	if filter.WarehouseID != uuid.Nil {
		scopes = append(scopes, base.Where("warehouse_id = ?", filter.WarehouseID))
	}
	if filter.LocationID != uuid.Nil {
		scopes = append(scopes, base.Where("location_id = ?", filter.LocationID))
	}
	if filter.MovementType != "" {
		scopes = append(scopes, base.Where("movement_type = ?", filter.MovementType))
	}
	if filter.ReferenceType != "" {
		scopes = append(scopes, base.Where("reference_type = ?", filter.ReferenceType))
	}
	if filter.ReferenceID != uuid.Nil {
		scopes = append(scopes, base.Where("reference_id = ?", filter.ReferenceID))
	}

	// Half-open range: From inclusive, To exclusive, so consecutive periods tile
	// without counting a boundary entry twice.
	if filter.OccurredFrom != nil {
		scopes = append(scopes, base.Where("occurred_at >= ?", *filter.OccurredFrom))
	}
	if filter.OccurredTo != nil {
		scopes = append(scopes, base.Where("occurred_at < ?", *filter.OccurredTo))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.InventoryLedgerEntry]{}, err
	}
	return pagination.Page[*entity.InventoryLedgerEntry]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}
