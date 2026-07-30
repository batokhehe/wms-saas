package repository

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	base "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ListFilter narrows a purchase-order listing. A plain struct rather than a DTO:
// the repository must not depend on the transport contract.
type ListFilter struct {
	Paging pagination.Request

	Status      string
	SupplierID  uuid.UUID
	WarehouseID uuid.UUID

	// Half-open range over order_date: From inclusive, To exclusive, so
	// consecutive periods tile without double-counting a boundary document.
	OrderedFrom *time.Time
	OrderedTo   *time.Time
}

// Repository is the persistence contract for the PurchaseOrder aggregate.
//
// Every method takes a companyID, because purchase orders are tenant-owned:
// RepositoryConvention §3 makes the tenant a required argument so forgetting it
// does not compile. The signatures speak in *entity.PurchaseOrder and plain
// filters — GORM stops at this boundary.
type Repository interface {
	// Create persists a new aggregate together with its lines.
	Create(ctx context.Context, o *entity.PurchaseOrder) error

	// Update persists the header under optimistic locking and REPLACES the line
	// set, so the stored lines always match the aggregate's.
	Update(ctx context.Context, o *entity.PurchaseOrder) error

	// FindByID returns one order with its lines, or NOT_FOUND. An order belonging
	// to another company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it
	// exists.
	FindByID(ctx context.Context, orderID, companyID uuid.UUID) (*entity.PurchaseOrder, error)

	// FindByNumber resolves an order by its operator-facing number.
	FindByNumber(ctx context.Context, companyID uuid.UUID, number string) (*entity.PurchaseOrder, error)

	// List returns a page of the company's orders, lines included.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.PurchaseOrder], error)

	// DeleteDraft removes a DRAFT order outright. It refuses any other status:
	// an approved order has been communicated to a supplier and a received one
	// has stock behind it, so both are cancelled rather than erased.
	DeleteDraft(ctx context.Context, orderID, companyID uuid.UUID) error

	// ExistsByNumber reports whether a number is taken within the company. It
	// backs the UniqueOrderNumber specification.
	ExistsByNumber(ctx context.Context, companyID uuid.UUID, number string) (bool, error)
}

type purchaseOrderRepository struct {
	repo *base.Base[purchaseOrderModel, *purchaseOrderModel]
}

var _ Repository = (*purchaseOrderRepository)(nil)

// New builds the repository over the generic base, parameterised on the
// PERSISTENCE model. The base is held as an unexported field, not embedded, so
// its CRUD over purchaseOrderModel is not promoted onto this type.
func New(db *gorm.DB, ids port.IDGenerator) Repository {
	return &purchaseOrderRepository{
		repo: base.New[purchaseOrderModel, *purchaseOrderModel](db, ids, "purchaseorder.repository"),
	}
}

// forTenant is the scope every method applies.
func forTenant(companyID uuid.UUID) base.Scope { return base.ForCompany(companyID) }

func (r *purchaseOrderRepository) Create(ctx context.Context, o *entity.PurchaseOrder) error {
	if err := r.repo.Create(ctx, toModel(o)); err != nil {
		return err
	}
	return r.writeLines(ctx, o)
}

// Update writes the header optimistically, then replaces the lines.
//
// Order matters: the optimistic header update runs FIRST, so a concurrent writer
// is detected before any line is touched. Both statements run in whatever
// transaction the caller opened, so a failure at either point rolls back both.
func (r *purchaseOrderRepository) Update(ctx context.Context, o *entity.PurchaseOrder) error {
	if err := r.repo.UpdateOptimistic(ctx, toModel(o)); err != nil {
		return err
	}
	if err := r.repo.DB(ctx).
		Where("purchase_order_id = ?", o.ID()).
		Delete(&purchaseOrderLineModel{}).Error; err != nil {
		return apperror.Internal("failed to replace purchase order lines").
			WithOp("purchaseorder.repository.Update").WithCause(err)
	}
	return r.writeLines(ctx, o)
}

func (r *purchaseOrderRepository) writeLines(ctx context.Context, o *entity.PurchaseOrder) error {
	rows := toLineModels(o)
	if len(rows) == 0 {
		return nil
	}
	if err := r.repo.DB(ctx).Create(&rows).Error; err != nil {
		return apperror.Internal("failed to persist purchase order lines").
			WithOp("purchaseorder.repository.writeLines").WithCause(err)
	}
	return nil
}

func (r *purchaseOrderRepository) FindByID(
	ctx context.Context, orderID, companyID uuid.UUID,
) (*entity.PurchaseOrder, error) {
	model, err := r.repo.FindByID(ctx, orderID, forTenant(companyID))
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

func (r *purchaseOrderRepository) FindByNumber(
	ctx context.Context, companyID uuid.UUID, number string,
) (*entity.PurchaseOrder, error) {
	model, err := r.repo.FindOne(ctx,
		forTenant(companyID),
		base.Where("number = ?", normalizeNumber(number)),
	)
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, model)
}

func (r *purchaseOrderRepository) List(
	ctx context.Context, companyID uuid.UUID, filter ListFilter,
) (pagination.Page[*entity.PurchaseOrder], error) {
	var empty pagination.Page[*entity.PurchaseOrder]

	scopes := []base.Scope{forTenant(companyID)}
	if filter.Status != "" {
		scopes = append(scopes, base.Where("status = ?", filter.Status))
	}
	if filter.SupplierID != uuid.Nil {
		scopes = append(scopes, base.Where("supplier_id = ?", filter.SupplierID))
	}
	if filter.WarehouseID != uuid.Nil {
		scopes = append(scopes, base.Where("warehouse_id = ?", filter.WarehouseID))
	}
	if filter.OrderedFrom != nil {
		scopes = append(scopes, base.Where("order_date >= ?", *filter.OrderedFrom))
	}
	if filter.OrderedTo != nil {
		scopes = append(scopes, base.Where("order_date < ?", *filter.OrderedTo))
	}
	if filter.Paging.HasSearch() {
		scopes = append(scopes, base.Search(filter.Paging.Search, "purchase_orders.number"))
	}

	page, err := r.repo.FindAll(ctx, filter.Paging, scopes...)
	if err != nil {
		return empty, err
	}

	// Lines for the whole page are fetched in ONE query rather than one per
	// order: a 25-row page would otherwise cost 26 round trips.
	ids := make([]uuid.UUID, 0, len(page.Items))
	for i := range page.Items {
		ids = append(ids, page.Items[i].ID)
	}
	grouped, err := r.linesFor(ctx, ids)
	if err != nil {
		return empty, err
	}

	orders := make([]*entity.PurchaseOrder, 0, len(page.Items))
	for i := range page.Items {
		model := page.Items[i]
		order, err := toDomain(&model, grouped[model.ID])
		if err != nil {
			return empty, err
		}
		orders = append(orders, order)
	}
	return pagination.Page[*entity.PurchaseOrder]{Items: orders, Meta: page.Meta}, nil
}

func (r *purchaseOrderRepository) DeleteDraft(
	ctx context.Context, orderID, companyID uuid.UUID,
) error {
	const op = "purchaseorder.repository.DeleteDraft"

	// The status is re-checked in SQL rather than trusted from a loaded
	// aggregate: between the service's check and this statement another request
	// could have approved the order, and deleting an approved order would erase a
	// commitment a supplier has already been told about.
	result := r.repo.DB(ctx).
		Where("id = ? AND company_id = ? AND status = ? AND deleted_at IS NULL",
			orderID, companyID, entity.StatusDraft.String()).
		Delete(&purchaseOrderModel{})
	if result.Error != nil {
		return apperror.Internal("failed to delete purchase order").
			WithOp(op).WithCause(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.NotFound("no draft purchase order with this id exists in this company").
			WithOp(op)
	}

	// The lines' FK is ON DELETE CASCADE, but the parent is SOFT deleted, so the
	// cascade never fires. Removing them here keeps an orphaned line set from
	// outliving the document it belonged to.
	if err := r.repo.DB(ctx).
		Where("purchase_order_id = ?", orderID).
		Delete(&purchaseOrderLineModel{}).Error; err != nil {
		return apperror.Internal("failed to delete purchase order lines").
			WithOp(op).WithCause(err)
	}
	return nil
}

func (r *purchaseOrderRepository) ExistsByNumber(
	ctx context.Context, companyID uuid.UUID, number string,
) (bool, error) {
	return r.repo.ExistsBy(ctx,
		forTenant(companyID),
		base.Where("number = ?", normalizeNumber(number)),
	)
}

// hydrate loads one order's lines and rebuilds the aggregate.
func (r *purchaseOrderRepository) hydrate(
	ctx context.Context, model *purchaseOrderModel,
) (*entity.PurchaseOrder, error) {
	grouped, err := r.linesFor(ctx, []uuid.UUID{model.ID})
	if err != nil {
		return nil, err
	}
	return toDomain(model, grouped[model.ID])
}

// linesFor loads the lines of every given order, grouped by order id.
func (r *purchaseOrderRepository) linesFor(
	ctx context.Context, orderIDs []uuid.UUID,
) (map[uuid.UUID][]purchaseOrderLineModel, error) {
	grouped := make(map[uuid.UUID][]purchaseOrderLineModel, len(orderIDs))
	if len(orderIDs) == 0 {
		return grouped, nil
	}

	var rows []purchaseOrderLineModel
	if err := r.repo.DB(ctx).
		Where("purchase_order_id IN ?", orderIDs).
		Order("created_at ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, apperror.Internal("failed to load purchase order lines").
			WithOp("purchaseorder.repository.linesFor").WithCause(err)
	}
	for _, row := range rows {
		grouped[row.PurchaseOrderID] = append(grouped[row.PurchaseOrderID], row)
	}
	return grouped, nil
}

// normalizeNumber canonicalises a number for comparison. The column is CITEXT so
// comparison is already case-insensitive; upper-casing keeps the query value
// consistent with what entity.NewOrderNumber stores.
func normalizeNumber(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }
