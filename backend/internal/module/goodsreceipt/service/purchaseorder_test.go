package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/dto"
	poentity "github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// These tests cover the Goods Receipt -> Purchase Order integration: a receipt
// that references an order must drive that order's received quantities, its
// outstanding quantities and its status, inside the receipt's own transaction.

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type harness struct {
	svc    *Service
	repo   *fakeRepo
	tx     *fakeTxManager
	stock  *fakeStockPoster
	orders *fakePurchaseOrders
	events *fakeEventPublisher
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	repo := newFakeRepo()
	stock := &fakeStockPoster{}
	orders := newFakePurchaseOrders()
	events := &fakeEventPublisher{}
	tx := &fakeTxManager{repo: repo, stock: stock, orders: orders}

	svc := New(repo, NewAcceptAnyWarehouse(), NewAcceptAnyLocation(), NewAcceptAnyProduct(),
		stock, orders,
		adapterclock.NewFakeAt("2026-08-18T10:00:00Z"), adapterid.NewSequential(), tx, events)

	return &harness{svc: svc, repo: repo, tx: tx, stock: stock, orders: orders, events: events}
}

func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

// orderFor builds an APPROVED purchase order with one line per supplied product
// quantity, and registers it with the fake.
func (h *harness) orderFor(t *testing.T, companyID uuid.UUID, ordered map[uuid.UUID]int64) *poentity.PurchaseOrder {
	t.Helper()
	actor := uuid.New()
	now := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)

	number, err := poentity.NewOrderNumber("PO-INT-1")
	if err != nil {
		t.Fatal(err)
	}
	order, err := poentity.NewPurchaseOrder(uuid.New(), companyID, number,
		uuid.New(), uuid.New(), now, now.AddDate(0, 0, 7), "", actor, now)
	if err != nil {
		t.Fatal(err)
	}

	lines := make([]poentity.PurchaseOrderLine, 0, len(ordered))
	for productID, quantity := range ordered {
		line, err := poentity.NewPurchaseOrderLine(
			uuid.New(), productID, uuid.New(), poentity.MustQuantity(quantity), poentity.NoMoney(), "")
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := order.ReplaceLines(lines, actor, now); err != nil {
		t.Fatal(err)
	}
	if err := order.Approve(actor, now); err != nil {
		t.Fatal(err)
	}
	order.PullEvents()

	h.orders.add(order)
	return order
}

// receiptLine builds one goods-receipt line request.
func receiptLine(productID uuid.UUID, quantity int64) dto.LineRequest {
	return dto.LineRequest{
		ProductID:  productID,
		LocationID: uuid.New(),
		UOMID:      uuid.New(),
		Quantity:   quantity,
	}
}

// receiveAgainst drafts, confirms and receives a goods receipt referencing an
// order, returning the error from the final step.
func (h *harness) receiveAgainst(
	t *testing.T, ctx context.Context, orderID *uuid.UUID, lines ...dto.LineRequest,
) error {
	t.Helper()

	req := dto.CreateGoodsReceiptRequest{
		Number:      "GR-" + uuid.NewString()[:8],
		WarehouseID: uuid.New(),
		ReceiptDate: time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
		Lines:       lines,
	}
	if orderID != nil {
		req.ReferenceType = string(poReference)
		req.ReferenceID = orderID
	}

	created, err := h.svc.Create(ctx, req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := h.svc.Confirm(ctx, created.ID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	_, err = h.svc.Receive(ctx, created.ID)
	return err
}

const poReference = "PURCHASE_ORDER"

// lineFor returns the order's line for a product.
func lineFor(t *testing.T, order *poentity.PurchaseOrder, productID uuid.UUID) poentity.PurchaseOrderLine {
	t.Helper()
	for _, line := range order.Lines() {
		if line.ProductID() == productID {
			return line
		}
	}
	t.Fatalf("order has no line for product %s", productID)
	return poentity.PurchaseOrderLine{}
}

// assertLine checks a line's received and outstanding quantities.
func assertLine(
	t *testing.T, order *poentity.PurchaseOrder, productID uuid.UUID,
	wantReceived, wantOutstanding int64,
) {
	t.Helper()
	line := lineFor(t, order, productID)
	if line.ReceivedQty().Value() != wantReceived {
		t.Errorf("received = %d, want %d", line.ReceivedQty().Value(), wantReceived)
	}
	if line.RemainingQty().Value() != wantOutstanding {
		t.Errorf("outstanding = %d, want %d", line.RemainingQty().Value(), wantOutstanding)
	}
}

// ---------------------------------------------------------------------------
// Partial and full receipt
// ---------------------------------------------------------------------------

func TestPartialReceiptLeavesOrderPartiallyReceived(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 100})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 40)); err != nil {
		t.Fatal(err)
	}

	updated := h.orders.get(id)
	if updated.Status() != poentity.StatusPartiallyReceived {
		t.Errorf("status = %q, want PARTIALLY_RECEIVED", updated.Status())
	}
	assertLine(t, updated, product, 40, 60)
	if updated.IsFullyReceived() {
		t.Error("an order with 60 outstanding reported as fully received")
	}
}

func TestFullReceiptCompletesTheOrder(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 75})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 75)); err != nil {
		t.Fatal(err)
	}

	updated := h.orders.get(id)
	if updated.Status() != poentity.StatusCompleted {
		t.Errorf("status = %q, want COMPLETED", updated.Status())
	}
	assertLine(t, updated, product, 75, 0)
	if !updated.IsFullyReceived() {
		t.Error("a fully received order does not report as such")
	}
}

// TestSuccessiveReceiptsAccumulate pins that outstanding shrinks across receipts
// and the status only reaches COMPLETED when the last unit lands.
func TestSuccessiveReceiptsAccumulate(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 50})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 20)); err != nil {
		t.Fatal(err)
	}
	assertLine(t, h.orders.get(id), product, 20, 30)
	if h.orders.get(id).Status() != poentity.StatusPartiallyReceived {
		t.Fatalf("after the first receipt status = %q", h.orders.get(id).Status())
	}

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 30)); err != nil {
		t.Fatal(err)
	}
	assertLine(t, h.orders.get(id), product, 50, 0)
	if h.orders.get(id).Status() != poentity.StatusCompleted {
		t.Errorf("after the second receipt status = %q, want COMPLETED", h.orders.get(id).Status())
	}
}

// ---------------------------------------------------------------------------
// Multi-line
// ---------------------------------------------------------------------------

func TestMultiLinePartialReceipt(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	first, second := uuid.New(), uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{first: 100, second: 60})
	id := order.ID()

	// One line fully satisfied, the other only partly: at least one line still has
	// outstanding quantity, so the order must be PARTIALLY_RECEIVED, not COMPLETED.
	if err := h.receiveAgainst(t, ctx, &id,
		receiptLine(first, 100),
		receiptLine(second, 25),
	); err != nil {
		t.Fatal(err)
	}

	updated := h.orders.get(id)
	if updated.Status() != poentity.StatusPartiallyReceived {
		t.Errorf("status = %q, want PARTIALLY_RECEIVED", updated.Status())
	}
	assertLine(t, updated, first, 100, 0)
	assertLine(t, updated, second, 25, 35)
}

func TestMultiLineCompletedReceipt(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{first: 10, second: 20, third: 30})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id,
		receiptLine(first, 10),
		receiptLine(second, 20),
		receiptLine(third, 30),
	); err != nil {
		t.Fatal(err)
	}

	updated := h.orders.get(id)
	if updated.Status() != poentity.StatusCompleted {
		t.Errorf("status = %q, want COMPLETED", updated.Status())
	}
	for product, ordered := range map[uuid.UUID]int64{first: 10, second: 20, third: 30} {
		assertLine(t, updated, product, ordered, 0)
	}
	if updated.TotalReceivedQty() != 60 {
		t.Errorf("total received = %d, want 60", updated.TotalReceivedQty())
	}
}

// TestRepeatedProductOnOneReceiptIsSummed pins the aggregation: two batches of
// one article arriving together are two receipt lines but ONE order line, so the
// order is told once with the combined quantity.
func TestRepeatedProductOnOneReceiptIsSummed(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 100})
	id := order.ID()

	first := receiptLine(product, 30)
	first.BatchNumber = "B-1"
	second := receiptLine(product, 45)
	second.BatchNumber = "B-2"

	if err := h.receiveAgainst(t, ctx, &id, first, second); err != nil {
		t.Fatal(err)
	}

	if calls := h.orders.callCount(); calls != 1 {
		t.Errorf("the order was told %d times, want 1 combined call", calls)
	}
	if recorded := h.orders.recorded(); len(recorded) == 1 && recorded[0].Quantity != 75 {
		t.Errorf("combined quantity = %d, want 75", recorded[0].Quantity)
	}
	assertLine(t, h.orders.get(id), product, 75, 25)
}

// ---------------------------------------------------------------------------
// Duplicate prevention
// ---------------------------------------------------------------------------

// TestDuplicateReceiveIsRefusedAndDoesNotDoubleCount is the safeguard against the
// worst failure mode: posting one delivery twice.
func TestDuplicateReceiveIsRefusedAndDoesNotDoubleCount(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 100})
	id := order.ID()

	created, err := h.svc.Create(ctx, dto.CreateGoodsReceiptRequest{
		Number:        "GR-DUP-1",
		WarehouseID:   uuid.New(),
		ReferenceType: poReference,
		ReferenceID:   &id,
		ReceiptDate:   time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
		Lines:         []dto.LineRequest{receiptLine(product, 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Confirm(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Receive(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	arrivalsAfterFirst := h.stock.count()
	callsAfterFirst := h.orders.callCount()

	// The receipt is RECEIVED and terminal, so the second attempt is refused by
	// the aggregate before anything is posted.
	if _, err := h.svc.Receive(ctx, created.ID); err == nil {
		t.Fatal("a RECEIVED goods receipt was received a second time")
	}

	assertLine(t, h.orders.get(id), product, 40, 60)
	if h.stock.count() != arrivalsAfterFirst {
		t.Errorf("the refused retry posted stock: %d -> %d", arrivalsAfterFirst, h.stock.count())
	}
	if h.orders.callCount() != callsAfterFirst {
		t.Errorf("the refused retry told the order again: %d -> %d", callsAfterFirst, h.orders.callCount())
	}
}

// TestOverReceiptIsRefusedByTheOrder pins that the order's own rule reaches back
// through the integration: a receipt for more than was ordered fails the receipt.
func TestOverReceiptIsRefusedByTheOrder(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 10})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 25)); err == nil {
		t.Fatal("a receipt for more than was ordered was accepted")
	}
	assertLine(t, h.orders.get(id), product, 0, 10)
	if h.orders.get(id).Status() != poentity.StatusApproved {
		t.Errorf("a refused receipt changed the status to %q", h.orders.get(id).Status())
	}
}

// ---------------------------------------------------------------------------
// Transactionality
// ---------------------------------------------------------------------------

// TestRollbackWhenPurchaseOrderUpdateFails is the core guarantee: the document,
// the stock and the order commit together or not at all.
func TestRollbackWhenPurchaseOrderUpdateFails(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 100})
	id := order.ID()

	created, err := h.svc.Create(ctx, dto.CreateGoodsReceiptRequest{
		Number:        "GR-ROLLBACK",
		WarehouseID:   uuid.New(),
		ReferenceType: poReference,
		ReferenceID:   &id,
		ReceiptDate:   time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
		Lines:         []dto.LineRequest{receiptLine(product, 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Confirm(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	rollbacksBefore := h.tx.rollbacks
	h.orders.fail(errInfrastructure)

	if _, err := h.svc.Receive(ctx, created.ID); err == nil {
		t.Fatal("the receipt was received despite the purchase order refusing")
	}

	if h.tx.rollbacks != rollbacksBefore+1 {
		t.Errorf("rollbacks = %d, want %d", h.tx.rollbacks, rollbacksBefore+1)
	}
	// The stock posted before the order was told must have been rolled back too.
	if h.stock.count() != 0 {
		t.Errorf("stock survived the rollback: %d arrivals", h.stock.count())
	}
	// The receipt must still be CONFIRMED, not RECEIVED.
	current, err := h.svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "CONFIRMED" {
		t.Errorf("receipt status = %q, want CONFIRMED after rollback", current.Status)
	}
	if current.ReceivedBy != nil {
		t.Error("a rolled-back receipt recorded a receiver")
	}
	assertLine(t, h.orders.get(id), product, 0, 100)
}

// TestStockFailureLeavesTheOrderUntouched covers the other order of failure: the
// order must not be told about an arrival whose stock never landed.
func TestStockFailureLeavesTheOrderUntouched(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 100})
	id := order.ID()

	h.stock.fail(errInfrastructure)
	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 40)); err == nil {
		t.Fatal("the receipt succeeded despite inventory posting failing")
	}

	if h.orders.callCount() != 0 {
		t.Errorf("the order was told about %d arrivals that never landed", h.orders.callCount())
	}
	assertLine(t, h.orders.get(id), product, 0, 100)
}

// TestReceiptStockAndOrderCommitTogether is the positive case of the same
// guarantee: on success all three participants moved.
func TestReceiptStockAndOrderCommitTogether(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	product := uuid.New()
	order := h.orderFor(t, company, map[uuid.UUID]int64{product: 60})
	id := order.ID()

	if err := h.receiveAgainst(t, ctx, &id, receiptLine(product, 60)); err != nil {
		t.Fatal(err)
	}

	// 1. the document
	if !h.events.has("goodsreceipt.received") {
		t.Error("no received event: the document did not reach RECEIVED")
	}
	// 2. the stock (which is what appends the ledger entry downstream)
	if h.stock.count() != 1 {
		t.Errorf("stock arrivals = %d, want 1", h.stock.count())
	}
	// 3. the order
	if h.orders.callCount() != 1 {
		t.Errorf("order updates = %d, want 1", h.orders.callCount())
	}
	if h.orders.get(id).Status() != poentity.StatusCompleted {
		t.Errorf("order status = %q, want COMPLETED", h.orders.get(id).Status())
	}
	if h.tx.rollbacks != 0 {
		t.Errorf("a successful receive rolled back %d times", h.tx.rollbacks)
	}
}

// ---------------------------------------------------------------------------
// Non-purchase-order receipts
// ---------------------------------------------------------------------------

// TestManualReceiptNeverTouchesAnOrder pins that the integration is inert for a
// receipt raised against no planning document — the commonest case for a
// transfer-in or a customer return.
func TestManualReceiptNeverTouchesAnOrder(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	if err := h.receiveAgainst(t, ctx, nil, receiptLine(uuid.New(), 5)); err != nil {
		t.Fatal(err)
	}
	if h.orders.callCount() != 0 {
		t.Errorf("a manual receipt told a purchase order %d times", h.orders.callCount())
	}
	if h.stock.count() != 1 {
		t.Errorf("a manual receipt posted %d arrivals, want 1", h.stock.count())
	}
}

// TestUnwiredReceiverRefusesAPurchaseOrderReceipt pins the safe default: without
// a wired adapter a receipt against an order fails loudly rather than silently
// leaving the order stale.
func TestUnwiredReceiverRefusesAPurchaseOrderReceipt(t *testing.T) {
	repo := newFakeRepo()
	stock := &fakeStockPoster{}
	tx := &fakeTxManager{repo: repo, stock: stock}
	svc := New(repo, nil, nil, nil, stock, nil,
		adapterclock.NewFakeAt("2026-08-18T10:00:00Z"), adapterid.NewSequential(),
		tx, &fakeEventPublisher{})

	ctx := scoped(uuid.New(), uuid.New())
	orderID := uuid.New()
	created, err := svc.Create(ctx, dto.CreateGoodsReceiptRequest{
		Number:        "GR-UNWIRED",
		WarehouseID:   uuid.New(),
		ReferenceType: poReference,
		ReferenceID:   &orderID,
		ReceiptDate:   time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC),
		Lines:         []dto.LineRequest{receiptLine(uuid.New(), 5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Confirm(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Receive(ctx, created.ID); err == nil {
		t.Fatal("an unwired purchase-order receiver silently accepted the receipt")
	}
}
