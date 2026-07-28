package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ListFilter narrows a customer listing.
type ListFilter struct {
	Paging pagination.Request
	Status string
}

// Repository is the persistence contract for the Customer aggregate. Every
// method takes a companyID, because customers are tenant-owned.
type Repository interface {
	// Save persists a new aggregate.
	Save(ctx context.Context, c *entity.Customer) error

	// Update persists changes to an existing aggregate under optimistic locking.
	Update(ctx context.Context, c *entity.Customer) error

	// FindByID returns one customer, or NOT_FOUND.
	FindByID(ctx context.Context, customerID, companyID uuid.UUID) (*entity.Customer, error)

	// FindByCode resolves a customer by its operator-facing code.
	FindByCode(ctx context.Context, companyID uuid.UUID, code string) (*entity.Customer, error)

	// List returns a page of the company's customers.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Customer], error)

	// ExistsByCode reports whether a code is taken within the company. It backs
	// the UniqueCustomerCode specification.
	ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
}

type customerRepository struct {
	repo *base.Base[customerModel, *customerModel]
}

var _ Repository = (*customerRepository)(nil)

// New builds the repository over the generic base.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &customerRepository{
		repo: base.New[customerModel, *customerModel](db, ids, "customer.repository"),
	}
}

// forTenant is the scope every method applies.
func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

func (r *customerRepository) Save(ctx context.Context, c *entity.Customer) error {
	return r.repo.Create(ctx, toModel(c))
}

func (r *customerRepository) Update(ctx context.Context, c *entity.Customer) error {
	return r.repo.UpdateOptimistic(ctx, toModel(c))
}

func (r *customerRepository) FindByID(
	ctx context.Context, customerID, companyID uuid.UUID,
) (*entity.Customer, error) {
	model, err := r.repo.FindByID(ctx, customerID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *customerRepository) FindByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (*entity.Customer, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *customerRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.Customer], error) {
	scopes := []base.Scope{forTenant(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search, "customers.code", "customers.name"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Customer]{}, err
	}
	return pagination.Page[*entity.Customer]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

func (r *customerRepository) ExistsByCode(
	ctx context.Context, companyID uuid.UUID, code string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("code = ?", normalizeCode(code)),
	)
}

// normalizeCode canonicalises a code for comparison, matching what
// entity.NewCustomerCode stores.
func normalizeCode(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }
