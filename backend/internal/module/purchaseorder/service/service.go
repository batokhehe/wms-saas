// Package service orchestrates the PurchaseOrder aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.PurchaseOrder. The service resolves the tenant and
// actor, verifies cross-aggregate references through its verifiers, opens ONE
// transaction, calls ONE aggregate behaviour, persists, commits, and only then
// publishes.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedport "github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// transition is a single lifecycle call on a loaded order.
type transition func(*entity.PurchaseOrder, uuid.UUID, time.Time) error

// Service orchestrates the PurchaseOrder aggregate.
type Service struct {
	repo repository.Repository

	suppliers  SupplierVerifier
	warehouses WarehouseVerifier
	products   ProductVerifier

	clock  sharedport.Clock
	ids    sharedport.IDGenerator
	tx     transaction.Manager
	events EventPublisher
}

// New builds the service. Nil verifiers fall back to the NAMED permissive
// defaults rather than to nil, so an unwired dependency cannot be mistaken for
// "the verifier permits it".
func New(
	repo repository.Repository,
	suppliers SupplierVerifier,
	warehouses WarehouseVerifier,
	products ProductVerifier,
	clock sharedport.Clock,
	ids sharedport.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	if suppliers == nil {
		suppliers = NewAcceptAnySupplier()
	}
	if warehouses == nil {
		warehouses = NewAcceptAnyWarehouse()
	}
	if products == nil {
		products = NewAcceptAnyProduct()
	}
	return &Service{
		repo: repo, suppliers: suppliers, warehouses: warehouses, products: products,
		clock: clock, ids: ids, tx: tx, events: events,
	}
}

// actor resolves the tenant and the acting user together. Both come from the
// RequestContext, never from a request field, so a client cannot name a company
// or impersonate a user.
func (s *Service) actor(ctx context.Context) (companyID, userID uuid.UUID, err error) {
	rc := appcontext.From(ctx)
	if userID, err = rc.RequireUser(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if companyID, err = rc.RequireTenant(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return companyID, userID, nil
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create drafts a purchase order.
func (s *Service) Create(ctx context.Context, req dto.CreatePurchaseOrderRequest) (dto.PurchaseOrderResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}

	number, err := entity.NewOrderNumber(req.Number)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}

	var order *entity.PurchaseOrder

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Uniqueness is checked INSIDE the transaction so a concurrent draft of
		// the same number cannot slip between the check and the insert. The
		// partial unique index is the real guarantee; this produces the friendly
		// 409 instead of a driver error.
		if err := s.assertNumberIsFree(ctx, companyID, number); err != nil {
			return err
		}
		if err := s.verifyParties(ctx, companyID, req.SupplierID, req.WarehouseID); err != nil {
			return err
		}

		now := s.clock.Now()
		built, err := entity.NewPurchaseOrder(
			s.ids.NewID(), companyID, number,
			req.SupplierID, req.WarehouseID,
			req.OrderDate, req.ExpectedArrivalDate,
			req.Remarks, actorID, now,
		)
		if err != nil {
			return err
		}

		if len(req.Lines) > 0 {
			lines, err := s.buildLines(ctx, companyID, req.Lines)
			if err != nil {
				return err
			}
			if err := built.ReplaceLines(lines, actorID, now); err != nil {
				return err
			}
		}

		if err := s.repo.Create(ctx, built); err != nil {
			return err
		}
		order = built
		return nil
	})
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}

	s.publish(ctx, order)
	return mapper.ToResponse(order), nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Update replaces a DRAFT order's editable state.
//
// The aggregate refuses the edit outside DRAFT; the service does not re-check the
// status, because duplicating the rule here is how the two drift apart.
func (s *Service) Update(
	ctx context.Context, orderID uuid.UUID, req dto.UpdatePurchaseOrderRequest,
) (dto.PurchaseOrderResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}

	var order *entity.PurchaseOrder

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.load(ctx, orderID, companyID)
		if err != nil {
			return err
		}
		if err := s.verifyParties(ctx, companyID, req.SupplierID, req.WarehouseID); err != nil {
			return err
		}

		now := s.clock.Now()
		if err := loaded.UpdateHeader(
			req.SupplierID, req.WarehouseID,
			req.OrderDate, req.ExpectedArrivalDate, req.Remarks, actorID, now,
		); err != nil {
			return err
		}

		lines, err := s.buildLines(ctx, companyID, req.Lines)
		if err != nil {
			return err
		}
		if err := loaded.ReplaceLines(lines, actorID, now); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		order = loaded
		return nil
	})
	if err != nil {
		return dto.PurchaseOrderResponse{}, s.concurrentModification(ctx, orderID, companyID, err)
	}

	s.publish(ctx, order)
	return mapper.ToResponse(order), nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Approve commits the order, unlocking the inbound chain.
func (s *Service) Approve(ctx context.Context, orderID uuid.UUID) (dto.PurchaseOrderResponse, error) {
	return s.mutate(ctx, orderID, "Approve",
		func(o *entity.PurchaseOrder, actor uuid.UUID, now time.Time) error {
			return o.Approve(actor, now)
		})
}

// Cancel withdraws the order.
func (s *Service) Cancel(
	ctx context.Context, orderID uuid.UUID, req dto.CancelPurchaseOrderRequest,
) (dto.PurchaseOrderResponse, error) {
	return s.mutate(ctx, orderID, "Cancel",
		func(o *entity.PurchaseOrder, actor uuid.UUID, now time.Time) error {
			return o.Cancel(req.Reason, actor, now)
		})
}

// RecordReceipt books an arrival against one line and re-derives the status.
//
// It is the IN-PROCESS entry point for the Goods Receipt flow, not an HTTP
// endpoint: received quantity is a consequence of goods physically arriving, and
// letting a client assert it directly would let the order claim deliveries that
// never happened. The aggregate refuses a receipt against a DRAFT or CANCELLED
// order, which is the rule that keeps receipts attached to real commitments.
func (s *Service) RecordReceipt(
	ctx context.Context, orderID, lineID uuid.UUID, quantity int64,
) (dto.PurchaseOrderResponse, error) {
	amount, err := entity.NewQuantity(quantity)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	return s.mutate(ctx, orderID, "RecordReceipt",
		func(o *entity.PurchaseOrder, actor uuid.UUID, now time.Time) error {
			return o.RecordReceipt(lineID, amount, actor, now)
		})
}

// RecordReceiptForProduct books an arrival against the line carrying a product.
//
// # Why by product rather than by line id
//
// A Goods Receipt names WHAT arrived, not which planning row it satisfies — its
// lines carry a product, never a purchase-order line id. Resolving the line here
// keeps that lookup inside the module that owns the rule making it unambiguous:
// an order may list a product at most once (ux_purchase_order_lines_order_product),
// so a product identifies exactly one line or none.
//
// Like RecordReceipt it is an IN-PROCESS entry point, not an HTTP endpoint, and it
// joins the caller's transaction — the Goods Receipt flow calls it while holding
// the transaction that also wrote the stock and the ledger entry.
func (s *Service) RecordReceiptForProduct(
	ctx context.Context, orderID, productID uuid.UUID, quantity int64,
) (dto.PurchaseOrderResponse, error) {
	amount, err := entity.NewQuantity(quantity)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	return s.mutate(ctx, orderID, "RecordReceiptForProduct",
		func(o *entity.PurchaseOrder, actor uuid.UUID, now time.Time) error {
			lineID, err := lineForProduct(o, productID)
			if err != nil {
				return err
			}
			return o.RecordReceipt(lineID, amount, actor, now)
		})
}

// lineForProduct finds the order line carrying a product.
//
// A receipt naming a product the order never asked for is REFUSED rather than
// ignored: silently dropping it would leave stock booked against an order that
// does not account for it, and the discrepancy would surface much later as an
// order that can never complete.
func lineForProduct(o *entity.PurchaseOrder, productID uuid.UUID) (uuid.UUID, error) {
	for _, line := range o.Lines() {
		if line.ProductID() == productID {
			return line.ID(), nil
		}
	}
	return uuid.Nil, apperror.NotFound("this purchase order has no line for the received product").
		WithOp("purchaseorder.service.lineForProduct").
		WithDetails(map[string]any{"product_id": productID})
}

// DeleteDraft removes a DRAFT order outright.
func (s *Service) DeleteDraft(ctx context.Context, orderID uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}
	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.repo.DeleteDraft(ctx, orderID, companyID)
	})
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// Get returns one purchase order.
func (s *Service) Get(ctx context.Context, orderID uuid.UUID) (dto.PurchaseOrderResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	order, err := s.repo.FindByID(ctx, orderID, companyID)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	return mapper.ToResponse(order), nil
}

// GetByNumber resolves an order by its operator-facing number.
func (s *Service) GetByNumber(ctx context.Context, number string) (dto.PurchaseOrderResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	order, err := s.repo.FindByNumber(ctx, companyID, number)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}
	return mapper.ToResponse(order), nil
}

// List returns a filtered, paginated page of the company's orders.
func (s *Service) List(
	ctx context.Context, query dto.ListPurchaseOrdersQuery,
) (pagination.Page[dto.PurchaseOrderResponse], error) {
	var empty pagination.Page[dto.PurchaseOrderResponse]

	companyID, _, err := s.actor(ctx)
	if err != nil {
		return empty, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return empty, err
	}

	from, err := query.ParseOrderedFrom()
	if err != nil {
		return empty, err
	}
	to, err := query.ParseOrderedTo()
	if err != nil {
		return empty, err
	}
	if from != nil && to != nil && !to.After(*from) {
		return empty, apperror.Validation("ordered_to must be after ordered_from").
			WithOp("purchaseorder.service.List")
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging:      query.Request,
		Status:      query.Status,
		SupplierID:  parseID(query.SupplierID),
		WarehouseID: parseID(query.WarehouseID),
		OrderedFrom: from,
		OrderedTo:   to,
	})
	if err != nil {
		return empty, err
	}
	return mapper.ToPage(page), nil
}

// ---------------------------------------------------------------------------
// Shared orchestration
// ---------------------------------------------------------------------------

// mutate is the shared shape of every load-modify-persist lifecycle operation:
// load, call one aggregate behaviour, persist — inside one transaction — then
// publish after the commit.
func (s *Service) mutate(
	ctx context.Context, orderID uuid.UUID, op string, apply transition,
) (dto.PurchaseOrderResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PurchaseOrderResponse{}, err
	}

	var order *entity.PurchaseOrder

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.load(ctx, orderID, companyID)
		if err != nil {
			return err
		}
		if err := apply(loaded, actorID, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		order = loaded
		return nil
	})
	if err != nil {
		return dto.PurchaseOrderResponse{}, s.concurrentModification(ctx, orderID, companyID, err)
	}

	s.publish(ctx, order)
	return mapper.ToResponse(order), nil
}

// load fetches an order and re-asserts its tenant.
//
// The repository already filtered by company; this is DEFENCE IN DEPTH and can
// only fire if that filter is ever broken.
func (s *Service) load(
	ctx context.Context, orderID, companyID uuid.UUID,
) (*entity.PurchaseOrder, error) {
	order, err := s.repo.FindByID(ctx, orderID, companyID)
	if err != nil {
		return nil, err
	}
	if !order.BelongsTo(companyID) {
		return nil, apperror.Forbidden("This purchase order belongs to another company").
			WithOp("purchaseorder.service.load")
	}
	return order, nil
}

// assertNumberIsFree is the UniqueOrderNumber specification.
func (s *Service) assertNumberIsFree(
	ctx context.Context, companyID uuid.UUID, number entity.OrderNumber,
) error {
	taken, err := s.repo.ExistsByNumber(ctx, companyID, number.String())
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("a purchase order with this number already exists").
			WithOp("purchaseorder.service.assertNumberIsFree").
			WithDetails(map[string]any{"number": number.String()})
	}
	return nil
}

// verifyParties runs the supplier and warehouse verifiers.
func (s *Service) verifyParties(ctx context.Context, companyID, supplierID, warehouseID uuid.UUID) error {
	if err := s.suppliers.VerifySupplier(ctx, companyID, supplierID); err != nil {
		return err
	}
	return s.warehouses.VerifyWarehouse(ctx, companyID, warehouseID)
}

// buildLines validates and constructs the aggregate's line set from a request.
func (s *Service) buildLines(
	ctx context.Context, companyID uuid.UUID, requested []dto.LineRequest,
) ([]entity.PurchaseOrderLine, error) {
	lines := make([]entity.PurchaseOrderLine, 0, len(requested))
	for _, item := range requested {
		if err := s.products.VerifyProduct(ctx, companyID, item.ProductID); err != nil {
			return nil, err
		}
		ordered, err := entity.NewQuantity(item.OrderedQty)
		if err != nil {
			return nil, err
		}
		price := entity.NoMoney()
		if item.UnitPrice != nil {
			price, err = entity.NewMoney(*item.UnitPrice)
			if err != nil {
				return nil, err
			}
		}
		line, err := entity.NewPurchaseOrderLine(
			s.ids.NewID(), item.ProductID, item.UOMID, ordered, price, item.Remarks,
		)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	return lines, nil
}

// concurrentModification turns the repository sentinel into the API's normal 409
// after rollback, carrying the current version as the retry token.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if err == nil || !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}
	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("purchaseorder.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// publish drains the aggregate's recorded events and publishes them. PullEvents
// clears as it reads, so a double publish is impossible; the service never
// constructs an event itself.
func (s *Service) publish(ctx context.Context, o *entity.PurchaseOrder) {
	if o == nil {
		return
	}
	for _, event := range o.PullEvents() {
		s.events.Publish(ctx, event)
	}
}

// parseID converts a validated uuid string into a uuid, or uuid.Nil when empty.
func parseID(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}
