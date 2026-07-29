package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

type ListFilter struct {
	Paging pagination.Request
	Status string
}

type Repository interface {
	Save(ctx context.Context, b *entity.Brand) error
	Update(ctx context.Context, b *entity.Brand) error
	FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Brand, error)
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Brand], error)
	ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
}

type brandRepository struct {
	repo *base.Base[brandModel, *brandModel]
}

var _ Repository = (*brandRepository)(nil)

func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &brandRepository{
		repo: base.New[brandModel, *brandModel](db, ids, "brand.repository"),
	}
}

func (r *brandRepository) Save(ctx context.Context, b *entity.Brand) error {
	return r.repo.Create(ctx, toModel(b))
}

func (r *brandRepository) Update(ctx context.Context, b *entity.Brand) error {
	return r.repo.UpdateOptimistic(ctx, toModel(b))
}

func (r *brandRepository) FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Brand, error) {
	model, err := r.repo.FindByID(ctx, id, base.ForCompany(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *brandRepository) List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Brand], error) {
	scopes := []base.Scope{base.ForCompany(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Brand]{}, err
	}
	return pagination.Page[*entity.Brand]{Items: toDomainSlice(page.Items), Meta: page.Meta}, nil
}

func (r *brandRepository) ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error) {
	return r.repo.ExistsBy(ctx, base.ForCompany(companyID), base.Where("code = ?", strings.ToUpper(strings.TrimSpace(code))))
}
