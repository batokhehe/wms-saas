// Package repository is the goods-receipt module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. It translates between the aggregate (unexported
// fields) and persistence models GORM can map.
package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ListFilter narrows a goods-receipt listing. A plain struct rather than a DTO:
// the repository must not depend on the transport contract.
type ListFilter struct {
	Paging pagination.Request

	Status      string
	WarehouseID uuid.UUID
	SupplierID  uuid.UUID

	ReferenceType string
	ReferenceID   uuid.UUID

	// Half-open range over receipt_date: From inclusive, To exclusive.
	ReceivedFrom *time.Time
	ReceivedTo   *time.Time
}

// Repository is the persistence contract for the GoodsReceipt aggregate.
//
// Every method takes a companyID, because receipts are tenant-owned:
// RepositoryConvention §3 makes the tenant a required argument so forgetting it
// does not compile.
type Repository interface {
	// Create persists a new aggregate together with its lines.
	Create(ctx context.Context, g *entity.GoodsReceipt) error

	// Update persists the header under optimistic locking and REPLACES the line
	// set, so the stored lines always match the aggregate's.
	Update(ctx context.Context, g *entity.GoodsReceipt) error

	// FindByID returns one receipt with its lines, or NOT_FOUND. A receipt
	// belonging to another company is NOT_FOUND, never FORBIDDEN — a 403 would
	// confirm it exists.
	FindByID(ctx context.Context, receiptID, companyID uuid.UUID) (*entity.GoodsReceipt, error)

	// FindByNumber resolves a receipt by its operator-facing number.
	FindByNumber(ctx context.Context, companyID uuid.UUID, number string) (*entity.GoodsReceipt, error)

	// List returns a page of the company's receipts, lines included.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.GoodsReceipt], error)

	// DeleteDraft removes a DRAFT receipt outright. Any other status is refused:
	// a confirmed receipt is cancelled and a received one is immutable.
	DeleteDraft(ctx context.Context, receiptID, companyID uuid.UUID) error

	// ExistsByNumber reports whether a number is taken within the company.
	ExistsByNumber(ctx context.Context, companyID uuid.UUID, number string) (bool, error)
}

type goodsReceiptRepository struct {
	repo *base.Base[goodsReceiptModel, *goodsReceiptModel]
}

var _ Repository = (*goodsReceiptRepository)(nil)

// New builds the repository over the generic base, parameterised on the
// PERSISTENCE model. The base is held as an unexported field, not embedded, so
// its CRUD over goodsReceiptModel is not promoted onto this type.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &goodsReceiptRepository{
		repo: base.New[goodsReceiptModel, *goodsReceiptModel](db, ids, "goodsreceipt.repository"),
	}
}

func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

func (r *goodsReceiptRepository) Create(ctx context.Context, g *entity.GoodsReceipt) error {
	if err := r.repo.Create(ctx, toModel(g)); err != nil {
		return err
	}
	return r.writeLines(ctx, g)
}

// Update writes the header optimistically, then replaces the lines.
//
// Order matters: the optimistic header update runs FIRST, so a concurrent writer
// is detected before any line is touched. Both statements run in whatever
// transaction the caller opened, so a failure at either point rolls back both.
func (r *goodsReceiptRepository) Update(ctx context.Context, g *entity.GoodsReceipt) error {
	if err := r.repo.UpdateOptimistic(ctx, toModel(g)); err != nil {
		return err
	}
	if err := r.repo.DB(ctx).
		Where("goods_receipt_id = ?", g.ID()).
		Delete(&goodsReceiptLineModel{}).Error; err != nil {
		return apperror.Internal("failed to replace goods receipt lines").
			WithOp("goodsreceipt.repository.Update").WithCause(err)
	}
	return r.writeLines(ctx, g)
}

func (r *goodsReceiptRepository) writeLines(ctx context.Context, g *entity.GoodsReceipt) error {
	rows := toLineModels(g)
	if len(rows) == 0 {
		return nil
	}
	if err := r.repo.DB(ctx).Create(&rows).Error; err != nil {
		return apperror.Internal("failed to persist goods receipt lines").
			WithOp("goodsreceipt.repository.writeLines").WithCause(err)
	}
	return nil
}

func (r *goodsReceiptRepository) FindByID(
	ctx context.Context, receiptID, companyID uuid.UUID,
) (*entity.GoodsReceipt, error) {
	model, err := r.repo.FindByID(ctx, receiptID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

func (r *goodsReceiptRepository) FindByNumber(
	ctx context.Context, companyID uuid.UUID, number string,
) (*entity.GoodsReceipt, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("number = ?", normalizeNumber(number)),
	)
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

func (r *goodsReceiptRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.GoodsReceipt], error) {
	var empty pagination.Page[*entity.GoodsReceipt]

	scopes := []base.Scope{forTenant(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.WarehouseID != uuid.Nil {
		scopes = append(scopes, base.Where("warehouse_id = ?", filter.WarehouseID))
	}
	if filter.SupplierID != uuid.Nil {
		scopes = append(scopes, base.Where("supplier_id = ?", filter.SupplierID))
	}
	if filter.ReferenceType != "" {
		scopes = append(scopes, base.Where("reference_type = ?", filter.ReferenceType))
	}
	if filter.ReferenceID != uuid.Nil {
		scopes = append(scopes, base.Where("reference_id = ?", filter.ReferenceID))
	}
	if filter.ReceivedFrom != nil {
		scopes = append(scopes, base.Where("receipt_date >= ?", *filter.ReceivedFrom))
	}
	if filter.ReceivedTo != nil {
		scopes = append(scopes, base.Where("receipt_date < ?", *filter.ReceivedTo))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search, "goods_receipts.number"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return empty, err
	}

	// Lines for the whole page are fetched in ONE query rather than one per
	// receipt: a 25-row page would otherwise cost 26 round trips.
	ids := make([]uuid.UUID, 0, len(page.Items))
	for i := range page.Items {
		ids = append(ids, page.Items[i].ID)
	}
	grouped, err := r.linesFor(ctx, ids)
	if err != nil {
		return empty, err
	}

	receipts := make([]*entity.GoodsReceipt, 0, len(page.Items))
	for i := range page.Items {
		model := page.Items[i]
		receipt, err := toDomain(&model, grouped[model.ID])
		if err != nil {
			return empty, err
		}
		receipts = append(receipts, receipt)
	}
	return pagination.Page[*entity.GoodsReceipt]{Items: receipts, Meta: page.Meta}, nil
}

func (r *goodsReceiptRepository) DeleteDraft(
	ctx context.Context, receiptID, companyID uuid.UUID,
) error {
	const op = "goodsreceipt.repository.DeleteDraft"

	// The status is re-checked in SQL rather than trusted from a loaded
	// aggregate: between the service's check and this statement another request
	// could have confirmed the receipt.
	result := r.repo.DB(ctx).
		Where("id = ? AND company_id = ? AND status = ? AND deleted_at IS NULL",
			receiptID, companyID, entity.StatusDraft.String()).
		Delete(&goodsReceiptModel{})
	if result.Error != nil {
		return apperror.Internal("failed to delete goods receipt").
			WithOp(op).WithCause(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.NotFound("no draft goods receipt with this id exists in this company").
			WithOp(op)
	}

	// The lines' FK is ON DELETE CASCADE, but the parent is SOFT deleted, so the
	// cascade never fires.
	if err := r.repo.DB(ctx).
		Where("goods_receipt_id = ?", receiptID).
		Delete(&goodsReceiptLineModel{}).Error; err != nil {
		return apperror.Internal("failed to delete goods receipt lines").
			WithOp(op).WithCause(err)
	}
	return nil
}

func (r *goodsReceiptRepository) ExistsByNumber(
	ctx context.Context, companyID uuid.UUID, number string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("number = ?", normalizeNumber(number)),
	)
}

func (r *goodsReceiptRepository) hydrate(
	ctx context.Context, model *goodsReceiptModel,
) (*entity.GoodsReceipt, error) {
	grouped, err := r.linesFor(ctx, []uuid.UUID{model.ID})
	if err != nil {
		return nil, err
	}
	return toDomain(model, grouped[model.ID])
}

func (r *goodsReceiptRepository) linesFor(
	ctx context.Context, receiptIDs []uuid.UUID,
) (map[uuid.UUID][]goodsReceiptLineModel, error) {
	grouped := make(map[uuid.UUID][]goodsReceiptLineModel, len(receiptIDs))
	if len(receiptIDs) == 0 {
		return grouped, nil
	}

	var rows []goodsReceiptLineModel
	if err := r.repo.DB(ctx).
		Where("goods_receipt_id IN ?", receiptIDs).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, apperror.Internal("failed to load goods receipt lines").
			WithOp("goodsreceipt.repository.linesFor").WithCause(err)
	}
	for _, row := range rows {
		grouped[row.GoodsReceiptID] = append(grouped[row.GoodsReceiptID], row)
	}
	return grouped, nil
}

// normalizeNumber canonicalises a number for comparison, matching what
// entity.NewReceiptNumber stores.
func normalizeNumber(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }
