package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ListFilter narrows a warehouse listing.
//
// A plain struct rather than the module's DTO: the repository must not import
// dto, or the persistence layer would depend on the transport contract and a
// change to the API shape would ripple into SQL.
type ListFilter struct {
	Paging pagination.Request
	Status string
	Type   string
}

// Repository is the persistence contract for the Warehouse aggregate.
//
// Every method takes a companyID. Warehouses are tenant-owned, so
// RepositoryConvention §3 applies in its ordinary form: making the tenant a
// required argument means forgetting it does not compile.
//
// Note the signatures: they speak in *entity.Warehouse — the aggregate — and in
// plain filters. No *gorm.DB, no Scope, no persistence model. GORM stops at this
// boundary, and so does warehouseModel, which is unexported.
type Repository interface {
	// Save persists a new aggregate.
	Save(ctx context.Context, w *entity.Warehouse) error

	// Update persists changes to an existing aggregate.
	Update(ctx context.Context, w *entity.Warehouse) error

	// FindByID returns one warehouse, or NOT_FOUND. A warehouse belonging to
	// another company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it
	// exists.
	FindByID(ctx context.Context, warehouseID, companyID uuid.UUID) (*entity.Warehouse, error)

	// FindByCode resolves a warehouse by its operator-facing code.
	FindByCode(ctx context.Context, companyID uuid.UUID, code string) (*entity.Warehouse, error)

	List(
		ctx context.Context, companyID uuid.UUID, filter ListFilter,
	) (pagination.Page[*entity.Warehouse], error)

	// ExistsByCode reports whether a code is taken within the company.
	ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error)

	// ExistsByName reports whether a name is taken within the company.
	//
	// Name uniqueness is unusual and deliberate: an operator picks a
	// destination from a dropdown by name, so two warehouses called "Jakarta
	// Central" would make mis-shipping a matter of chance.
	ExistsByName(ctx context.Context, companyID uuid.UUID, name string) (bool, error)

	// ExistsByNameExcluding is the rename check: is this name taken by anyone
	// OTHER than the warehouse being renamed? Without the exclusion, renaming a
	// warehouse to its own current name would report a conflict.
	ExistsByNameExcluding(
		ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
	) (bool, error)

	// CountActive reports how many operational warehouses a company has.
	CountActive(ctx context.Context, companyID uuid.UUID) (int64, error)
}

type warehouseRepository struct {
	repo *base.Base[warehouseModel, *warehouseModel]
}

var _ Repository = (*warehouseRepository)(nil)

// New builds the repository.
//
// The generic base is parameterised over the PERSISTENCE model, not the
// aggregate — the base requires entity.Identifiable, which warehouseModel gets
// from BaseEntity and the aggregate deliberately does not have.
//
// It is held as an unexported FIELD rather than embedded, so the base's CRUD is
// not promoted onto this type. Embedding would expose Create/Update/Delete
// operating on warehouseModel, letting a caller bypass the aggregate entirely.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &warehouseRepository{
		repo: base.New[warehouseModel, *warehouseModel](db, ids, "warehouse.repository"),
	}
}

// forTenant is the scope every method below applies.
//
// Wrapping it in a named helper means a reviewer auditing this file for missing
// tenant filters is looking for one identifier, not for an inline Where clause
// that is easy to skim past.
func forTenant(companyID uuid.UUID) base.Scope {
	return base.ForCompany(companyID)
}

func (r *warehouseRepository) Save(ctx context.Context, w *entity.Warehouse) error {
	return r.repo.Create(ctx, toModel(w))
}

func (r *warehouseRepository) Update(ctx context.Context, w *entity.Warehouse) error {
	return r.repo.UpdateOptimistic(ctx, toModel(w))
}

func (r *warehouseRepository) FindByID(
	ctx context.Context, warehouseID, companyID uuid.UUID,
) (*entity.Warehouse, error) {
	model, err := r.repo.FindByID(ctx, warehouseID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *warehouseRepository) FindByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (*entity.Warehouse, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *warehouseRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.Warehouse], error) {
	scopes := []base.Scope{forTenant(companyID)}

	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Type != "" {
		scopes = append(scopes, base.Where("type = ?", filter.Type))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search,
			"warehouses.code", "warehouses.name"))
	}

	// The base rejects a pagination.Request that has not been through Apply, so
	// an unvalidated sort column can never reach the SQL.
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Warehouse]{}, err
	}

	// Translated to aggregates before leaving the repository, so no caller ever
	// holds a persistence model.
	return pagination.Page[*entity.Warehouse]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

func (r *warehouseRepository) ExistsByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
}

func (r *warehouseRepository) ExistsByName(
	ctx context.Context, companyID uuid.UUID, name string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("name = ?", normalizeName(name)),
	)
}

func (r *warehouseRepository) ExistsByNameExcluding(
	ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("name = ?", normalizeName(name)),
		base.Where("id <> ?", excludeID),
	)
}

func (r *warehouseRepository) CountActive(
	ctx context.Context, companyID uuid.UUID,
) (int64, error) {
	return r.repo.Count(ctx,
		forTenant(companyID),
		base.Where("status = ?", entity.StatusActive.String()),
	)
}

// normalizeCode canonicalises a code for comparison.
//
// The column is CITEXT so comparison is already case-insensitive; normalising
// here keeps the query value consistent with what entity.NewCode stores, so a
// lookup and an insert cannot disagree.
func normalizeCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// normalizeName canonicalises a name for comparison.
//
// Only trimmed, not upper-cased: names are CITEXT so case is already ignored,
// and upper-casing would make the query value differ from the stored display
// form for no benefit.
func normalizeName(raw string) string {
	return strings.TrimSpace(raw)
}
