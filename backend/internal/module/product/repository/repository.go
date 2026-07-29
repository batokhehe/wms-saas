package repository

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
)

type ListFilter struct {
	Paging pagination.Request
	Status string
}

type Repository interface {
	Save(ctx context.Context, p *entity.Product) error
	Update(ctx context.Context, p *entity.Product) error
	FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Product, error)
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Product], error)
	ExistsBySKU(ctx context.Context, companyID uuid.UUID, sku entity.SKU) (bool, error)
	ExistsByBarcode(ctx context.Context, companyID uuid.UUID, barcode string) (bool, error)
}

type productRepository struct {
	repo *base.Base[productModel, *productModel]
	db   *gorm.DB
}

var _ Repository = (*productRepository)(nil)

func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &productRepository{
		repo: base.New[productModel, *productModel](db, ids, "product.repository"),
		db:   db,
	}
}

func (r *productRepository) Save(ctx context.Context, p *entity.Product) error {
	return r.repo.Create(ctx, toModel(p))
}

func (r *productRepository) Update(ctx context.Context, p *entity.Product) error {
	return r.repo.UpdateOptimistic(ctx, toModel(p))
}

func (r *productRepository) FindByID(ctx context.Context, id, companyID uuid.UUID) (*entity.Product, error) {
	model, err := r.repo.FindByID(ctx, id, base.ForCompany(companyID))
	if err != nil {
		return nil, err
	}
	return toDomain(model), nil
}

func (r *productRepository) List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.Product], error) {
	scopes := []base.Scope{base.ForCompany(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return pagination.Page[*entity.Product]{}, err
	}
	return pagination.Page[*entity.Product]{Items: toDomainSlice(page.Items), Meta: page.Meta}, nil
}

func (r *productRepository) ExistsBySKU(ctx context.Context, companyID uuid.UUID, sku entity.SKU) (bool, error) {
	return r.repo.ExistsBy(ctx, base.ForCompany(companyID), base.Where("sku = ?", strings.ToUpper(strings.TrimSpace(string(sku)))))
}

func (r *productRepository) ExistsByBarcode(ctx context.Context, companyID uuid.UUID, barcode string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("product_barcodes").
		Joins("JOIN products ON products.id = product_barcodes.product_id").
		Where("products.company_id = ? AND product_barcodes.code = ?", companyID, strings.TrimSpace(barcode)).
		Count(&count).Error
	return count > 0, err
}
