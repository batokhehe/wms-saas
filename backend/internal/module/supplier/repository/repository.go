package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ListFilter narrows a supplier listing. A plain struct rather than a DTO: the
// repository must not depend on the transport contract.
type ListFilter struct {
	Paging pagination.Request
	Status string
}

// Repository is the persistence contract for the Supplier aggregate.
//
// Every method takes a companyID, because suppliers are tenant-owned:
// RepositoryConvention §3 makes the tenant a required argument so forgetting it
// does not compile. The signatures speak in *entity.Supplier and plain filters —
// GORM stops at this boundary, and so does supplierModel.
type Repository interface {
	// Save persists a new aggregate.
	Save(ctx context.Context, s *entity.Supplier) error

	// Update persists changes to an existing aggregate under optimistic locking.
	Update(ctx context.Context, s *entity.Supplier) error

	// FindByID returns one supplier, or NOT_FOUND. A supplier belonging to
	// another company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it
	// exists.
	FindByID(ctx context.Context, supplierID, companyID uuid.UUID) (*entity.Supplier, error)

	// FindByCode resolves a supplier by its operator-facing code.
	FindByCode(ctx context.Context, companyID uuid.UUID, code string) (*entity.Supplier, error)

	// List returns a page of the company's suppliers.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Supplier], error)

	// ExistsByCode reports whether a code is taken within the company. It backs
	// the UniqueSupplierCode specification.
	ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
}

type supplierRepository struct {
	repo *base.Base[supplierModel, *supplierModel]
}

var _ Repository = (*supplierRepository)(nil)

// New builds the repository over the generic base, parameterised on the
// PERSISTENCE model. The base is held as an unexported field, not embedded, so
// its CRUD over supplierModel is not promoted onto this type.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &supplierRepository{
		repo: base.New[supplierModel, *supplierModel](db, ids, "supplier.repository"),
	}
}

// forTenant is the scope every method applies.
func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

func (r *supplierRepository) Save(ctx context.Context, s *entity.Supplier) error {
	return r.repo.Create(ctx, toModel(s))
}

func (r *supplierRepository) Update(ctx context.Context, s *entity.Supplier) error {
	return r.repo.UpdateOptimistic(ctx, toModel(s))
}

func (r *supplierRepository) FindByID(
	ctx context.Context, supplierID, companyID uuid.UUID,
) (*entity.Supplier, error) {
	model, err := r.repo.FindByID(ctx, supplierID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *supplierRepository) FindByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (*entity.Supplier, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *supplierRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.Supplier], error) {
	scopes := []base.Scope{forTenant(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search, "suppliers.code", "suppliers.name"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Supplier]{}, err
	}
	return pagination.Page[*entity.Supplier]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

func (r *supplierRepository) ExistsByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
}

// normalizeCode canonicalises a code for comparison. The column is CITEXT so
// comparison is already case-insensitive; upper-casing keeps the query value
// consistent with what entity.NewSupplierCode stores.
func normalizeCode(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }
