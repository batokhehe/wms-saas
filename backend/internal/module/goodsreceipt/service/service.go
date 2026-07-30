// Package service orchestrates the GoodsReceipt aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.GoodsReceipt. The service resolves the tenant and
// actor, verifies cross-aggregate references through its verifiers, opens ONE
// transaction, calls ONE aggregate behaviour, persists, commits, and only then
// publishes.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedport "github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// errPurchaseOrderUpdatesUnwired is returned by the refusing default
// PurchaseOrderReceiver.
var errPurchaseOrderUpdatesUnwired = apperror.Internal(
	"purchase order updating is not wired; a receipt against an order cannot be received")

// errStockPostingUnwired is returned by the refusing default StockPoster.
var errStockPostingUnwired = apperror.Internal(
	"inventory posting is not wired; a goods receipt cannot be received")

// transition is a single lifecycle call on a loaded receipt.
type transition func(*entity.GoodsReceipt, uuid.UUID, time.Time) error

// Service orchestrates the GoodsReceipt aggregate.
type Service struct {
	repo repository.Repository

	warehouses WarehouseVerifier
	locations  LocationVerifier
	products   ProductVerifier
	stock      StockPoster
	orders     PurchaseOrderReceiver

	clock  sharedport.Clock
	ids    sharedport.IDGenerator
	tx     transaction.Manager
	events EventPublisher
}

// New builds the service. Nil collaborators fall back to NAMED defaults rather
// than to nil. Note the asymmetry: the verifiers default to permissive, but the
// stock poster defaults to REFUSING — a missing verifier degrades a check, while
// a missing poster would let a receipt claim stock it never created.
func New(
	repo repository.Repository,
	warehouses WarehouseVerifier,
	locations LocationVerifier,
	products ProductVerifier,
	stock StockPoster,
	orders PurchaseOrderReceiver,
	clock sharedport.Clock,
	ids sharedport.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	if warehouses == nil {
		warehouses = NewAcceptAnyWarehouse()
	}
	if locations == nil {
		locations = NewAcceptAnyLocation()
	}
	if products == nil {
		products = NewAcceptAnyProduct()
	}
	if stock == nil {
		stock = NewRefuseStockPosting()
	}
	if orders == nil {
		orders = NewRefusePurchaseOrderUpdates()
	}
	return &Service{
		repo: repo, warehouses: warehouses, locations: locations,
		products: products, stock: stock, orders: orders,
		clock: clock, ids: ids, tx: tx, events: events,
	}
}

// actor resolves the tenant and the acting user together. Both come from the
// RequestContext, never from a request field.
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
// Create / Update
// ---------------------------------------------------------------------------

// Create drafts a goods receipt.
func (s *Service) Create(ctx context.Context, req dto.CreateGoodsReceiptRequest) (dto.GoodsReceiptResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	number, err := entity.NewReceiptNumber(req.Number)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}
	reference, err := entity.NewDocumentReference(entity.ReferenceType(req.ReferenceType), req.ReferenceID)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	var receipt *entity.GoodsReceipt

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Uniqueness is checked INSIDE the transaction so a concurrent draft of
		// the same number cannot slip between the check and the insert. The
		// partial unique index is the real guarantee; this produces the friendly
		// 409 instead of a driver error.
		if err := s.assertNumberIsFree(ctx, companyID, number); err != nil {
			return err
		}
		if err := s.warehouses.VerifyWarehouse(ctx, companyID, req.WarehouseID); err != nil {
			return err
		}

		now := s.clock.Now()
		built, err := entity.NewGoodsReceipt(
			s.ids.NewID(), companyID, number, req.WarehouseID, req.SupplierID,
			reference, req.ReceiptDate, req.Remarks, actorID, now,
		)
		if err != nil {
			return err
		}

		if len(req.Lines) > 0 {
			lines, err := s.buildLines(ctx, companyID, req.WarehouseID, req.Lines)
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
		receipt = built
		return nil
	})
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	s.publish(ctx, receipt)
	return mapper.ToResponse(receipt), nil
}

// Update replaces a DRAFT receipt's editable state.
func (s *Service) Update(
	ctx context.Context, receiptID uuid.UUID, req dto.UpdateGoodsReceiptRequest,
) (dto.GoodsReceiptResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	reference, err := entity.NewDocumentReference(entity.ReferenceType(req.ReferenceType), req.ReferenceID)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	var receipt *entity.GoodsReceipt

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.load(ctx, receiptID, companyID)
		if err != nil {
			return err
		}
		if err := s.warehouses.VerifyWarehouse(ctx, companyID, req.WarehouseID); err != nil {
			return err
		}

		now := s.clock.Now()
		if err := loaded.UpdateHeader(
			req.WarehouseID, req.SupplierID, reference, req.ReceiptDate, req.Remarks, actorID, now,
		); err != nil {
			return err
		}

		lines, err := s.buildLines(ctx, companyID, req.WarehouseID, req.Lines)
		if err != nil {
			return err
		}
		if err := loaded.ReplaceLines(lines, actorID, now); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		receipt = loaded
		return nil
	})
	if err != nil {
		return dto.GoodsReceiptResponse{}, s.concurrentModification(ctx, receiptID, companyID, err)
	}

	s.publish(ctx, receipt)
	return mapper.ToResponse(receipt), nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Confirm checks a draft off for posting.
func (s *Service) Confirm(ctx context.Context, receiptID uuid.UUID) (dto.GoodsReceiptResponse, error) {
	return s.mutate(ctx, receiptID, "Confirm",
		func(g *entity.GoodsReceipt, actor uuid.UUID, now time.Time) error {
			return g.Confirm(actor, now)
		})
}

// Cancel abandons a receipt before posting.
func (s *Service) Cancel(
	ctx context.Context, receiptID uuid.UUID, req dto.CancelGoodsReceiptRequest,
) (dto.GoodsReceiptResponse, error) {
	return s.mutate(ctx, receiptID, "Cancel",
		func(g *entity.GoodsReceipt, actor uuid.UUID, now time.Time) error {
			return g.Cancel(req.Reason, actor, now)
		})
}

// Receive posts the receipt's stock into inventory and marks it RECEIVED.
//
// # Transaction shape
//
// The aggregate transition, every inventory posting and the persist all run in
// ONE transaction. If any single line fails to post, the whole thing rolls back:
// the receipt stays CONFIRMED and no partial stock lands. A receipt marked
// RECEIVED whose stock never arrived — or half arrived — would be worse than no
// receipt at all, because the ledger would disagree with the document forever.
//
// # Serial fan-out
//
// An inventory position individuated by a serial holds exactly one unit, so a
// line naming N serials becomes N postings of one unit each rather than a single
// posting of N. That is the inventory module's rule, honoured here rather than
// worked around.
func (s *Service) Receive(ctx context.Context, receiptID uuid.UUID) (dto.GoodsReceiptResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	var receipt *entity.GoodsReceipt

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.load(ctx, receiptID, companyID)
		if err != nil {
			return err
		}

		// The transition runs FIRST so an ineligible receipt is refused before any
		// stock moves — posting and then discovering the receipt was already
		// RECEIVED would have created inventory that nothing accounts for.
		if err := loaded.Receive(actorID, s.clock.Now()); err != nil {
			return err
		}

		for _, arrival := range arrivalsFor(loaded) {
			if err := s.stock.PostArrival(ctx, arrival); err != nil {
				return err
			}
		}

		// Still inside the same transaction as the document, the stock and the
		// ledger entries. If the order refuses the receipt — it was cancelled, or
		// it never listed this product, or the quantity would over-receive a line —
		// everything above rolls back with it.
		if err := s.recordAgainstPurchaseOrder(ctx, loaded); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		receipt = loaded
		return nil
	})
	if err != nil {
		return dto.GoodsReceiptResponse{}, s.concurrentModification(ctx, receiptID, companyID, err)
	}

	s.publish(ctx, receipt)
	return mapper.ToResponse(receipt), nil
}

// recordAgainstPurchaseOrder tells the referenced order what arrived.
//
// It is a no-op for a manual receipt, or one raised against an ASN: only a
// PURCHASE_ORDER reference identifies a document that tracks outstanding
// quantity. That check is the aggregate's (Reference().IsPurchaseOrder()), so the
// service does not reinterpret what counts as a purchase-order receipt.
//
// Quantities are SUMMED PER PRODUCT before the order is told. A receipt may
// legitimately carry the same product on two lines — different batches of one
// article arriving together — but the order lists that product once, so two
// separate calls would reload and re-save the order for what is one arrival.
// Summing keeps it to one update per product and one version bump.
func (s *Service) recordAgainstPurchaseOrder(ctx context.Context, g *entity.GoodsReceipt) error {
	reference := g.Reference()
	if !reference.IsPurchaseOrder() {
		return nil
	}
	orderID := *reference.ID()

	// Ordered rather than map-ranged: a map would make the sequence of updates —
	// and so the error a caller sees when several lines are wrong — depend on Go's
	// randomised map iteration.
	totals := make(map[uuid.UUID]int64, g.LineCount())
	order := make([]uuid.UUID, 0, g.LineCount())
	for _, line := range g.Lines() {
		if _, seen := totals[line.ProductID()]; !seen {
			order = append(order, line.ProductID())
		}
		totals[line.ProductID()] += line.Quantity().Value()
	}

	for _, productID := range order {
		if err := s.orders.RecordReceipt(ctx, PurchaseOrderReceipt{
			OrderID:   orderID,
			ProductID: productID,
			Quantity:  totals[productID],
		}); err != nil {
			return err
		}
	}
	return nil
}

// arrivalsFor expands a receipt's lines into unit-bearing inventory postings.
func arrivalsFor(g *entity.GoodsReceipt) []StockArrival {
	var arrivals []StockArrival
	for _, line := range g.Lines() {
		base := StockArrival{
			WarehouseID: g.WarehouseID(),
			LocationID:  line.LocationID(),
			ProductID:   line.ProductID(),
		}
		switch {
		case line.IsSerialTracked():
			for _, serial := range line.SerialNumbers() {
				arrival := base
				arrival.Tracking = "SERIAL"
				arrival.SerialNumber = serial
				arrival.Quantity = 1
				arrivals = append(arrivals, arrival)
			}
		case line.IsLotTracked():
			arrival := base
			arrival.Tracking = "LOT"
			arrival.LotNumber = line.LotNumber()
			arrival.Quantity = line.Quantity().Value()
			arrivals = append(arrivals, arrival)
		default:
			arrival := base
			arrival.Tracking = "NONE"
			arrival.Quantity = line.Quantity().Value()
			arrivals = append(arrivals, arrival)
		}
	}
	return arrivals
}

// DeleteDraft removes a DRAFT receipt outright.
func (s *Service) DeleteDraft(ctx context.Context, receiptID uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}
	return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.repo.DeleteDraft(ctx, receiptID, companyID)
	})
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// Get returns one goods receipt.
func (s *Service) Get(ctx context.Context, receiptID uuid.UUID) (dto.GoodsReceiptResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}
	receipt, err := s.repo.FindByID(ctx, receiptID, companyID)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}
	return mapper.ToResponse(receipt), nil
}

// GetByNumber resolves a receipt by its operator-facing number.
func (s *Service) GetByNumber(ctx context.Context, number string) (dto.GoodsReceiptResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}
	receipt, err := s.repo.FindByNumber(ctx, companyID, number)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}
	return mapper.ToResponse(receipt), nil
}

// List returns a filtered, paginated page of the company's receipts.
func (s *Service) List(
	ctx context.Context, query dto.ListGoodsReceiptsQuery,
) (pagination.Page[dto.GoodsReceiptResponse], error) {
	var empty pagination.Page[dto.GoodsReceiptResponse]

	companyID, _, err := s.actor(ctx)
	if err != nil {
		return empty, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return empty, err
	}

	from, err := query.ParseReceivedFrom()
	if err != nil {
		return empty, err
	}
	to, err := query.ParseReceivedTo()
	if err != nil {
		return empty, err
	}
	if from != nil && to != nil && !to.After(*from) {
		return empty, apperror.Validation("received_to must be after received_from").
			WithOp("goodsreceipt.service.List")
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging:        query.Request,
		Status:        query.Status,
		WarehouseID:   parseID(query.WarehouseID),
		SupplierID:    parseID(query.SupplierID),
		ReferenceType: query.ReferenceType,
		ReferenceID:   parseID(query.ReferenceID),
		ReceivedFrom:  from,
		ReceivedTo:    to,
	})
	if err != nil {
		return empty, err
	}
	return mapper.ToPage(page), nil
}

// ---------------------------------------------------------------------------
// Shared orchestration
// ---------------------------------------------------------------------------

func (s *Service) mutate(
	ctx context.Context, receiptID uuid.UUID, op string, apply transition,
) (dto.GoodsReceiptResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.GoodsReceiptResponse{}, err
	}

	var receipt *entity.GoodsReceipt

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.load(ctx, receiptID, companyID)
		if err != nil {
			return err
		}
		if err := apply(loaded, actorID, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		receipt = loaded
		return nil
	})
	if err != nil {
		return dto.GoodsReceiptResponse{}, s.concurrentModification(ctx, receiptID, companyID, err)
	}

	s.publish(ctx, receipt)
	return mapper.ToResponse(receipt), nil
}

// load fetches a receipt and re-asserts its tenant. The repository already
// filtered by company; this is DEFENCE IN DEPTH.
func (s *Service) load(
	ctx context.Context, receiptID, companyID uuid.UUID,
) (*entity.GoodsReceipt, error) {
	receipt, err := s.repo.FindByID(ctx, receiptID, companyID)
	if err != nil {
		return nil, err
	}
	if !receipt.BelongsTo(companyID) {
		return nil, apperror.Forbidden("This goods receipt belongs to another company").
			WithOp("goodsreceipt.service.load")
	}
	return receipt, nil
}

func (s *Service) assertNumberIsFree(
	ctx context.Context, companyID uuid.UUID, number entity.ReceiptNumber,
) error {
	taken, err := s.repo.ExistsByNumber(ctx, companyID, number.String())
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("a goods receipt with this number already exists").
			WithOp("goodsreceipt.service.assertNumberIsFree").
			WithDetails(map[string]any{"number": number.String()})
	}
	return nil
}

// buildLines validates and constructs the aggregate's line set from a request.
//
// Each line's location is verified against the receipt's WAREHOUSE, so goods
// cannot be booked into a bin that belongs to a different site.
func (s *Service) buildLines(
	ctx context.Context, companyID, warehouseID uuid.UUID, requested []dto.LineRequest,
) ([]entity.GoodsReceiptLine, error) {
	lines := make([]entity.GoodsReceiptLine, 0, len(requested))
	for _, item := range requested {
		if err := s.products.VerifyProduct(ctx, companyID, item.ProductID); err != nil {
			return nil, err
		}
		if err := s.locations.VerifyLocation(ctx, companyID, warehouseID, item.LocationID); err != nil {
			return nil, err
		}
		quantity, err := entity.NewQuantity(item.Quantity)
		if err != nil {
			return nil, err
		}
		line, err := entity.NewGoodsReceiptLine(
			s.ids.NewID(), item.ProductID, item.LocationID, item.UOMID, quantity,
			item.BatchNumber, item.LotNumber, item.SerialNumbers, item.ExpiryDate, item.Remarks,
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
		WithOp("goodsreceipt.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// publish drains the aggregate's recorded events and publishes them.
func (s *Service) publish(ctx context.Context, g *entity.GoodsReceipt) {
	if g == nil {
		return
	}
	for _, event := range g.PullEvents() {
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
