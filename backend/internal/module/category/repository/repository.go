package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

type ListFilter struct {
	Paging pagination.Request
	Status string
}

type Repository interface {
	Save(ctx context.Context, c *entity.Category) error
	Update(ctx context.Context, c *entity.Category) error
	FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Category, error)
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Category], error)
	ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error)
}

type categoryRepository struct {
	repo *base.Base[categoryModel, *categoryModel]
}

var _ Repository = (*categoryRepository)(nil)

func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &categoryRepository{
		repo: base.New[categoryModel, *categoryModel](db, ids, "category.repository"),
	}
}

func (r *categoryRepository) Save(ctx context.Context, c *entity.Category) error {
	return r.repo.Create(ctx, toModel(c))
}

func (r *categoryRepository) Update(ctx context.Context, c *entity.Category) error {
	return r.repo.UpdateOptimistic(ctx, toModel(c))
}

func (r *categoryRepository) FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Category, error) {
	model, err := r.repo.FindByID(ctx, id, base.ForCompany(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *categoryRepository) List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Category], error) {
	scopes := []base.Scope{base.ForCompany(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Category]{}, err
	}
	return pagination.Page[*entity.Category]{Items: toDomainSlice(page.Items), Meta: page.Meta}, nil
}

func (r *categoryRepository) ExistsByCode(ctx context.Context, companyID uuid.UUID, code string) (bool, error) {
	return r.repo.ExistsBy(ctx, base.ForCompany(companyID), base.Where("code = ?", strings.ToUpper(strings.TrimSpace(code))))
}
