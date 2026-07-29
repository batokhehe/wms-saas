// Package repository is the UOM module's persistence layer.
//
// It is the only UOM package that imports GORM or the shared repository
// implementation. Callers receive and provide aggregates only.
package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

// ListFilter narrows a UOM listing without importing an HTTP DTO into the
// persistence layer.
type ListFilter struct {
	Paging pagination.Request
	Status string
}

// Repository is the persistence contract for the globally shared UOM
// aggregate. Unlike warehouse and location, UOM is not company-scoped.
type Repository interface {
	Save(ctx context.Context, uom *entity.UOM) error
	Update(ctx context.Context, uom *entity.UOM) error

	FindByID(ctx context.Context, id uuid.UUID) (*entity.UOM, error)
	FindByCode(ctx context.Context, code string) (*entity.UOM, error)
	List(ctx context.Context, filter ListFilter) (pagination.Page[*entity.UOM], error)

	Exists(ctx context.Context, id uuid.UUID) (bool, error)
	ExistsByCode(ctx context.Context, code string) (bool, error)
}

type uomRepository struct {
	repo *base.Base[uomModel, *uomModel]
}

var _ Repository = (*uomRepository)(nil)

// New builds the UOM repository over the shared generic persistence base.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &uomRepository{
		repo: base.New[uomModel, *uomModel](db, ids, "uom.repository"),
	}
}

func (r *uomRepository) Save(ctx context.Context, uom *entity.UOM) error {
	return r.repo.Create(ctx, toModel(uom))
}

func (r *uomRepository) Update(ctx context.Context, uom *entity.UOM) error {
	return r.repo.UpdateOptimistic(ctx, toModel(uom))
}

func (r *uomRepository) FindByID(ctx context.Context, id uuid.UUID) (*entity.UOM, error) {
	model, err := r.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *uomRepository) FindByCode(ctx context.Context, code string) (*entity.UOM, error) {
	model, err := r.repo.FindOne(ctx, base.Where("code = ?", normalizeCode(code)))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *uomRepository) List(
	ctx context.Context,
	filter ListFilter,
) (pagination.Page[*entity.UOM], error) {
	scopes := make([]base.Scope, 0, 2)
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search, "uoms.code", "uoms.name"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.UOM]{}, err
	}

	return pagination.Page[*entity.UOM]{
		Items: toDomainSlice(page.Items),
		Meta:  page.Meta,
	}, nil
}

func (r *uomRepository) Exists(ctx context.Context, id uuid.UUID) (bool, error) {
	return r.repo.Exists(ctx, id)
}

func (r *uomRepository) ExistsByCode(ctx context.Context, code string) (bool, error) {
	return r.repo.ExistsBy(ctx, base.Where("code = ?", normalizeCode(code)))
}

func normalizeCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}
