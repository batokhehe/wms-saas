package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/repository"
	poentity "github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// The purchaseorder/entity import above is TEST-ONLY. Go compiles test imports
// into the test binary alone, so the production build graph stays free of the
// cross-module dependency ModuleConvention §6 forbids. It is here because a fake
// that merely records calls could not prove what this integration is for: that a
// receipt drives the ORDER's own status derivation. The fake below therefore
// holds a real PurchaseOrder aggregate and applies receipts to it.

var errInfrastructure = apperror.Internal("infrastructure failure")

func zapNop() *zap.Logger { return zap.NewNop() }

// ---------------------------------------------------------------------------
// fakeRepo
// ---------------------------------------------------------------------------

type fakeRepo struct {
	mu       sync.Mutex
	receipts map[uuid.UUID]*entity.GoodsReceipt
	failures map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		receipts: make(map[uuid.UUID]*entity.GoodsReceipt),
		failures: make(map[string]error),
	}
}

func (r *fakeRepo) fail(method string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures[method] = err
}

func (r *fakeRepo) check(method string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures[method]
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.receipts)
}

// snapshot copies the store so the fake transaction manager can restore it.
func (r *fakeRepo) snapshot() map[uuid.UUID]*entity.GoodsReceipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[uuid.UUID]*entity.GoodsReceipt, len(r.receipts))
	for id, receipt := range r.receipts {
		out[id] = receipt
	}
	return out
}

func (r *fakeRepo) restore(state map[uuid.UUID]*entity.GoodsReceipt) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts = state
}

func (r *fakeRepo) Create(_ context.Context, g *entity.GoodsReceipt) error {
	if err := r.check("Create"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts[g.ID()] = g
	return nil
}

func (r *fakeRepo) Update(_ context.Context, g *entity.GoodsReceipt) error {
	if err := r.check("Update"); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts[g.ID()] = g
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, receiptID, companyID uuid.UUID) (*entity.GoodsReceipt, error) {
	if err := r.check("FindByID"); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	receipt, ok := r.receipts[receiptID]
	if !ok || !receipt.BelongsTo(companyID) {
		return nil, apperror.NotFound("goods receipt not found")
	}
	// A COPY, as a real repository returns: it reconstitutes a fresh aggregate
	// from a row on every load. Handing back the stored pointer would let a
	// caller's in-memory mutation survive a rollback, which would quietly make
	// every rollback assertion in this package meaningless.
	return copyOf(receipt), nil
}

// copyOf reconstitutes an independent aggregate carrying the same state.
func copyOf(g *entity.GoodsReceipt) *entity.GoodsReceipt {
	copied, err := entity.Reconstitute(
		g.ID(), g.CompanyID(), g.Number(), g.WarehouseID(), g.SupplierID(),
		g.Reference(), g.ReceiptDate(), g.Status(), g.Remarks(), g.Lines(),
		g.Version(), g.CreatedBy(), g.ReceivedBy(), g.UpdatedBy(),
		g.CreatedAt(), g.UpdatedAt(),
	)
	if err != nil {
		// Unreachable for state that was valid when stored; returning the original
		// keeps a fake bug from masquerading as a service bug.
		return g
	}
	return copied
}

func (r *fakeRepo) FindByNumber(_ context.Context, companyID uuid.UUID, number string) (*entity.GoodsReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, receipt := range r.receipts {
		if receipt.BelongsTo(companyID) && receipt.Number().String() == number {
			return receipt, nil
		}
	}
	return nil, apperror.NotFound("goods receipt not found")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, _ repository.ListFilter) (pagination.Page[*entity.GoodsReceipt], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*entity.GoodsReceipt, 0, len(r.receipts))
	for _, receipt := range r.receipts {
		if receipt.BelongsTo(companyID) {
			items = append(items, receipt)
		}
	}
	return pagination.Page[*entity.GoodsReceipt]{Items: items}, nil
}

func (r *fakeRepo) DeleteDraft(_ context.Context, receiptID, _ uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.receipts, receiptID)
	return nil
}

func (r *fakeRepo) ExistsByNumber(_ context.Context, companyID uuid.UUID, number string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, receipt := range r.receipts {
		if receipt.BelongsTo(companyID) && receipt.Number().String() == number {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// fakeTxManager
// ---------------------------------------------------------------------------

// fakeTxManager mimics the real manager's SAVEPOINT behaviour closely enough to
// test rollback: a nested call joins the outer unit of work, and a failure at the
// outermost level restores every participant's pre-transaction state.
type fakeTxManager struct {
	repo      *fakeRepo
	stock     *fakeStockPoster
	orders    *fakePurchaseOrders
	calls     int
	rollbacks int
	depth     int
}

func (m *fakeTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	if m.depth > 0 {
		// Joined, not nested: the inner call shares the outer unit of work.
		return fn(ctx)
	}

	receipts := m.repo.snapshot()
	var arrivals []StockArrival
	var orders purchaseOrderState
	if m.stock != nil {
		arrivals = m.stock.all()
	}
	if m.orders != nil {
		orders = m.orders.snapshot()
	}

	m.depth++
	err := fn(ctx)
	m.depth--

	if err != nil {
		m.rollbacks++
		m.repo.restore(receipts)
		if m.stock != nil {
			m.stock.restore(arrivals)
		}
		if m.orders != nil {
			m.orders.restore(orders)
		}
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// fakeStockPoster
// ---------------------------------------------------------------------------

type fakeStockPoster struct {
	mu       sync.Mutex
	arrivals []StockArrival
	err      error
}

var _ StockPoster = (*fakeStockPoster)(nil)

func (p *fakeStockPoster) PostArrival(_ context.Context, arrival StockArrival) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.arrivals = append(p.arrivals, arrival)
	return nil
}

func (p *fakeStockPoster) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *fakeStockPoster) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.arrivals)
}

func (p *fakeStockPoster) all() []StockArrival {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]StockArrival, len(p.arrivals))
	copy(out, p.arrivals)
	return out
}

func (p *fakeStockPoster) restore(state []StockArrival) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.arrivals = state
}

// ---------------------------------------------------------------------------
// fakePurchaseOrders
// ---------------------------------------------------------------------------

// fakePurchaseOrders stands in for the purchase-order application service and
// holds REAL PurchaseOrder aggregates, so a receipt genuinely drives the order's
// own quantity arithmetic and status derivation rather than a mock's opinion of
// them.
type fakePurchaseOrders struct {
	mu     sync.Mutex
	orders map[uuid.UUID]*poentity.PurchaseOrder
	calls  []PurchaseOrderReceipt
	err    error
	actor  uuid.UUID
	now    time.Time
}

var _ PurchaseOrderReceiver = (*fakePurchaseOrders)(nil)

func newFakePurchaseOrders() *fakePurchaseOrders {
	return &fakePurchaseOrders{
		orders: make(map[uuid.UUID]*poentity.PurchaseOrder),
		actor:  uuid.New(),
		now:    time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
	}
}

func (f *fakePurchaseOrders) add(order *poentity.PurchaseOrder) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders[order.ID()] = order
}

func (f *fakePurchaseOrders) get(id uuid.UUID) *poentity.PurchaseOrder {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orders[id]
}

func (f *fakePurchaseOrders) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakePurchaseOrders) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakePurchaseOrders) recorded() []PurchaseOrderReceipt {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]PurchaseOrderReceipt, len(f.calls))
	copy(out, f.calls)
	return out
}

// purchaseOrderState is everything a rollback has to put back.
type purchaseOrderState struct {
	orders map[uuid.UUID]*poentity.PurchaseOrder
	calls  []PurchaseOrderReceipt
}

// snapshot deep-copies every order by reconstituting it, so a rollback genuinely
// restores the quantities rather than handing back the same mutated pointer. The
// call log is captured alongside, so rolling back one transaction does not erase
// the record of an earlier committed one.
func (f *fakePurchaseOrders) snapshot() purchaseOrderState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[uuid.UUID]*poentity.PurchaseOrder, len(f.orders))
	for id, order := range f.orders {
		copied, err := poentity.Reconstitute(
			order.ID(), order.CompanyID(), order.Number(),
			order.SupplierID(), order.WarehouseID(),
			order.OrderDate(), order.ExpectedArrivalDate(),
			order.Status(), order.Remarks(), order.Lines(),
			order.Version(), order.CreatedBy(), order.ApprovedBy(), order.ApprovedAt(),
			order.UpdatedBy(), order.CreatedAt(), order.UpdatedAt(),
		)
		if err != nil {
			continue
		}
		out[id] = copied
	}
	calls := make([]PurchaseOrderReceipt, len(f.calls))
	copy(calls, f.calls)
	return purchaseOrderState{orders: out, calls: calls}
}

func (f *fakePurchaseOrders) restore(state purchaseOrderState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if state.orders != nil {
		f.orders = state.orders
	}
	f.calls = state.calls
}

// RecordReceipt resolves the line carrying the product and applies the arrival,
// mirroring what purchaseorder.Service.RecordReceiptForProduct does.
func (f *fakePurchaseOrders) RecordReceipt(_ context.Context, receipt PurchaseOrderReceipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, receipt)

	order, ok := f.orders[receipt.OrderID]
	if !ok {
		return apperror.NotFound("purchase order not found")
	}

	amount, err := poentity.NewQuantity(receipt.Quantity)
	if err != nil {
		return err
	}
	for _, line := range order.Lines() {
		if line.ProductID() == receipt.ProductID {
			return order.RecordReceipt(line.ID(), amount, f.actor, f.now)
		}
	}
	return apperror.NotFound("this purchase order has no line for the received product")
}

// ---------------------------------------------------------------------------
// fakeEventPublisher
// ---------------------------------------------------------------------------

type fakeEventPublisher struct {
	mu     sync.Mutex
	events []entity.Event
}

var _ EventPublisher = (*fakeEventPublisher)(nil)

func (p *fakeEventPublisher) Publish(_ context.Context, event entity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *fakeEventPublisher) has(name entity.EventName) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.events {
		if e.Name == name {
			return true
		}
	}
	return false
}

func (p *fakeEventPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}
