package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) {
	return uuid.New(), time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
}

func mustNumber(t *testing.T, raw string) OrderNumber {
	t.Helper()
	number, err := NewOrderNumber(raw)
	if err != nil {
		t.Fatalf("NewOrderNumber(%q) = %v", raw, err)
	}
	return number
}

// mustLine builds a line for a fresh product ordering the given quantity.
func mustLine(t *testing.T, ordered int64) PurchaseOrderLine {
	t.Helper()
	line, err := NewPurchaseOrderLine(
		uuid.New(), uuid.New(), uuid.New(), MustQuantity(ordered), NoMoney(), "",
	)
	if err != nil {
		t.Fatalf("NewPurchaseOrderLine = %v", err)
	}
	return line
}

// draft opens a DRAFT order and discards the creation event.
func draft(t *testing.T) *PurchaseOrder {
	t.Helper()
	actor, now := act()
	order, err := NewPurchaseOrder(
		uuid.New(), uuid.New(), mustNumber(t, "PO-001"),
		uuid.New(), uuid.New(), now, now.AddDate(0, 0, 7), "", actor, now,
	)
	if err != nil {
		t.Fatalf("NewPurchaseOrder = %v", err)
	}
	order.PullEvents()
	return order
}

// approved returns an APPROVED order carrying one line of the given quantity.
func approved(t *testing.T, ordered int64) (*PurchaseOrder, uuid.UUID) {
	t.Helper()
	order := draft(t)
	actor, now := act()
	line := mustLine(t, ordered)
	if err := order.ReplaceLines([]PurchaseOrderLine{line}, actor, now); err != nil {
		t.Fatalf("ReplaceLines = %v", err)
	}
	if err := order.Approve(actor, now); err != nil {
		t.Fatalf("Approve = %v", err)
	}
	order.PullEvents()
	return order, line.ID()
}

func hasEvent(order *PurchaseOrder, name EventName) bool {
	for _, event := range order.PullEvents() {
		if event.Name == name {
			return true
		}
	}
	return false
}

// ---------- Factory ----------

func TestNewPurchaseOrderStartsDraft(t *testing.T) {
	actor, now := act()
	company := uuid.New()

	order, err := NewPurchaseOrder(
		uuid.New(), company, mustNumber(t, "po-001"),
		uuid.New(), uuid.New(), now, now.AddDate(0, 0, 3), "urgent", actor, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if order.Status() != StatusDraft || !order.IsEditable() {
		t.Errorf("status = %q, want an editable DRAFT", order.Status())
	}
	if order.Version() != 1 || order.LineCount() != 0 {
		t.Error("a new order should be version 1 with no lines")
	}
	if order.Number().String() != "PO-001" {
		t.Errorf("number = %q, want canonicalised PO-001", order.Number())
	}
	if order.ApprovedBy() != nil || order.ApprovedAt() != nil {
		t.Error("a draft must not carry approval metadata")
	}
	if !order.BelongsTo(company) {
		t.Error("order does not belong to its own company")
	}
	events := order.PullEvents()
	if len(events) != 1 || events[0].Name != EventOrderCreated {
		t.Fatalf("expected exactly one created event, got %+v", events)
	}
}

func TestNewPurchaseOrderRejectsBadInput(t *testing.T) {
	actor, now := act()
	number := mustNumber(t, "PO-002")
	supplier, warehouse := uuid.New(), uuid.New()
	later := now.AddDate(0, 0, 5)

	cases := map[string]func() error{
		"nil id": func() error {
			_, err := NewPurchaseOrder(uuid.Nil, uuid.New(), number, supplier, warehouse, now, later, "", actor, now)
			return err
		},
		"nil company": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.Nil, number, supplier, warehouse, now, later, "", actor, now)
			return err
		},
		"nil actor": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), number, supplier, warehouse, now, later, "", uuid.Nil, now)
			return err
		},
		"empty number": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), OrderNumber{}, supplier, warehouse, now, later, "", actor, now)
			return err
		},
		"nil supplier": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), number, uuid.Nil, warehouse, now, later, "", actor, now)
			return err
		},
		"nil warehouse": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), number, supplier, uuid.Nil, now, later, "", actor, now)
			return err
		},
		"zero order date": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), number, supplier, warehouse, time.Time{}, later, "", actor, now)
			return err
		},
		"arrival before order date": func() error {
			_, err := NewPurchaseOrder(uuid.New(), uuid.New(), number, supplier, warehouse, now, now.AddDate(0, 0, -1), "", actor, now)
			return err
		},
	}
	for label, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

// ---------- Reconstitution ----------

func TestReconstituteRaisesNoEvents(t *testing.T) {
	actor, now := act()
	approver := uuid.New()
	order, err := Reconstitute(
		uuid.New(), uuid.New(), mustNumber(t, "PO-003"), uuid.New(), uuid.New(),
		now, now, StatusApproved, "", []PurchaseOrderLine{mustLine(t, 10)},
		4, actor, &approver, &now, actor, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := order.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if order.Version() != 4 || order.Status() != StatusApproved || order.LineCount() != 1 {
		t.Error("reconstituted state wrong")
	}
	if order.ApprovedBy() == nil || *order.ApprovedBy() != approver {
		t.Error("approval metadata did not survive reconstitution")
	}
}

// TestApprovedByPointerIsDefensivelyCopied proves a caller cannot rewrite the
// aggregate through the pointer it handed in or the one it got back.
func TestApprovedByPointerIsDefensivelyCopied(t *testing.T) {
	actor, now := act()
	approver := uuid.New()
	order, err := Reconstitute(
		uuid.New(), uuid.New(), mustNumber(t, "PO-004"), uuid.New(), uuid.New(),
		now, now, StatusApproved, "", nil, 1, actor, &approver, &now, actor, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}

	approver = uuid.New() // mutate the caller's variable
	if got := order.ApprovedBy(); got == nil || *got == approver {
		t.Fatal("the order's approver changed when the caller's variable did")
	}
	returned := order.ApprovedBy()
	*returned = uuid.New()
	if again := order.ApprovedBy(); *again == *returned {
		t.Fatal("mutating the returned pointer changed the aggregate")
	}
}

// ---------- Editability ----------

// TestApprovedOrderCannotBeEdited is the module's central lock.
func TestApprovedOrderCannotBeEdited(t *testing.T) {
	order, _ := approved(t, 10)
	actor, now := act()

	if err := order.ReplaceLines([]PurchaseOrderLine{mustLine(t, 5)}, actor, now); err == nil {
		t.Error("lines were replaced on an APPROVED order")
	}
	if err := order.UpdateHeader(uuid.New(), uuid.New(), now, now, "changed", actor, now); err == nil {
		t.Error("the header was edited on an APPROVED order")
	}
	if order.LineCount() != 1 {
		t.Errorf("a refused edit changed the line count to %d", order.LineCount())
	}
}

func TestEveryNonDraftStateIsLocked(t *testing.T) {
	actor, now := act()

	partial, lineID := approved(t, 10)
	if err := partial.RecordReceipt(lineID, MustQuantity(4), actor, now); err != nil {
		t.Fatal(err)
	}
	completed, doneLine := approved(t, 3)
	if err := completed.RecordReceipt(doneLine, MustQuantity(3), actor, now); err != nil {
		t.Fatal(err)
	}
	cancelled, _ := approved(t, 2)
	if err := cancelled.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}

	for label, order := range map[string]*PurchaseOrder{
		"PARTIALLY_RECEIVED": partial,
		"COMPLETED":          completed,
		"CANCELLED":          cancelled,
	} {
		if order.IsEditable() {
			t.Errorf("%s should not be editable", label)
		}
		if err := order.ReplaceLines(nil, actor, now); err == nil {
			t.Errorf("%s accepted a line replacement", label)
		}
	}
}

func TestDraftAcceptsHeaderAndLineEdits(t *testing.T) {
	order := draft(t)
	actor, now := act()
	supplier, warehouse := uuid.New(), uuid.New()

	if err := order.UpdateHeader(supplier, warehouse, now, now.AddDate(0, 0, 2), "revised", actor, now); err != nil {
		t.Fatal(err)
	}
	if order.SupplierID() != supplier || order.WarehouseID() != warehouse || order.Remarks() != "revised" {
		t.Error("header edit did not apply")
	}
	if !hasEvent(order, EventOrderUpdated) {
		t.Error("no updated event after a header edit")
	}
	if err := order.ReplaceLines([]PurchaseOrderLine{mustLine(t, 1), mustLine(t, 2)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if order.LineCount() != 2 || order.TotalOrderedQty() != 3 {
		t.Errorf("lines = %d, total ordered = %d; want 2 and 3", order.LineCount(), order.TotalOrderedQty())
	}
}

// ---------- Approval ----------

func TestApproveRequiresAtLeastOneLine(t *testing.T) {
	order := draft(t)
	actor, now := act()

	if err := order.Approve(actor, now); err == nil {
		t.Fatal("an empty order was approved")
	}
	if order.Status() != StatusDraft {
		t.Errorf("a refused approval changed the status to %q", order.Status())
	}
}

func TestApproveRecordsApprover(t *testing.T) {
	order := draft(t)
	actor, now := act()
	if err := order.ReplaceLines([]PurchaseOrderLine{mustLine(t, 5)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if err := order.Approve(actor, now); err != nil {
		t.Fatal(err)
	}
	if order.Status() != StatusApproved {
		t.Fatalf("status = %q, want APPROVED", order.Status())
	}
	if order.ApprovedBy() == nil || *order.ApprovedBy() != actor {
		t.Error("approver not recorded")
	}
	if order.ApprovedAt() == nil || !order.ApprovedAt().Equal(now) {
		t.Error("approval time not recorded")
	}
	if !hasEvent(order, EventOrderApproved) {
		t.Error("no approved event")
	}
}

func TestApproveIsRefusedFromEveryOtherState(t *testing.T) {
	actor, now := act()

	twice, _ := approved(t, 5)
	if err := twice.Approve(actor, now); err == nil {
		t.Error("an APPROVED order was approved again")
	}

	cancelled, _ := approved(t, 5)
	if err := cancelled.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}
	if err := cancelled.Approve(actor, now); err == nil {
		t.Error("a CANCELLED order was approved")
	}
}

// ---------- Receiving ----------

// TestGoodsCannotBeReceivedAgainstADraft is the inbound-chain rule: a receipt
// must never attach to a document the supplier was never committed to.
func TestGoodsCannotBeReceivedAgainstADraft(t *testing.T) {
	order := draft(t)
	actor, now := act()
	line := mustLine(t, 10)
	if err := order.ReplaceLines([]PurchaseOrderLine{line}, actor, now); err != nil {
		t.Fatal(err)
	}

	if err := order.RecordReceipt(line.ID(), MustQuantity(1), actor, now); err == nil {
		t.Fatal("a receipt was booked against a DRAFT order")
	}
	if order.IsOpenForReceipt() {
		t.Error("a DRAFT must not report as open for receipt")
	}
	if order.TotalReceivedQty() != 0 {
		t.Error("a refused receipt changed a quantity")
	}
}

func TestGoodsCannotBeReceivedAgainstACancelledOrder(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()
	if err := order.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}
	if err := order.RecordReceipt(lineID, MustQuantity(1), actor, now); err == nil {
		t.Error("a receipt was booked against a CANCELLED order")
	}
}

// TestPartialReceiptDerivesStatus pins that PARTIALLY_RECEIVED and COMPLETED are
// computed from the lines, never chosen.
func TestPartialReceiptDerivesStatus(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()

	if err := order.RecordReceipt(lineID, MustQuantity(4), actor, now); err != nil {
		t.Fatal(err)
	}
	if order.Status() != StatusPartiallyReceived {
		t.Fatalf("status = %q, want PARTIALLY_RECEIVED", order.Status())
	}
	if order.Lines()[0].RemainingQty().Value() != 6 {
		t.Errorf("remaining = %d, want 6", order.Lines()[0].RemainingQty().Value())
	}
	if !order.IsOpenForReceipt() || !order.CanGenerateASN() {
		t.Error("a partially received order should still accept goods and generate an ASN")
	}

	if err := order.RecordReceipt(lineID, MustQuantity(6), actor, now); err != nil {
		t.Fatal(err)
	}
	if order.Status() != StatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", order.Status())
	}
	if !order.IsFullyReceived() || order.Lines()[0].RemainingQty().Value() != 0 {
		t.Error("the order should be fully received with nothing remaining")
	}
	if !hasEvent(order, EventOrderCompleted) {
		t.Error("no completed event when the last unit arrived")
	}
}

// TestOverReceiptIsRefusedNotClamped pins that the excess is rejected rather
// than silently discarded.
func TestOverReceiptIsRefusedNotClamped(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()

	if err := order.RecordReceipt(lineID, MustQuantity(11), actor, now); err == nil {
		t.Fatal("an over-receipt was accepted")
	}
	if order.TotalReceivedQty() != 0 || order.Status() != StatusApproved {
		t.Error("a refused over-receipt changed state")
	}

	if err := order.RecordReceipt(lineID, MustQuantity(7), actor, now); err != nil {
		t.Fatal(err)
	}
	// 7 + 4 = 11 > 10: the cumulative check must catch it too.
	if err := order.RecordReceipt(lineID, MustQuantity(4), actor, now); err == nil {
		t.Error("a cumulative over-receipt was accepted")
	}
	if order.TotalReceivedQty() != 7 {
		t.Errorf("received = %d, want 7", order.TotalReceivedQty())
	}
}

func TestReceiptRejectsUnknownLineAndZeroQuantity(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()

	if err := order.RecordReceipt(uuid.New(), MustQuantity(1), actor, now); err == nil {
		t.Error("a receipt against an unknown line was accepted")
	}
	if err := order.RecordReceipt(lineID, MustQuantity(0), actor, now); err == nil {
		t.Error("a zero-quantity receipt was accepted")
	}
}

// ---------- Cancellation ----------

func TestCancelIsAllowedFromDraftAndApproved(t *testing.T) {
	actor, now := act()

	pending := draft(t)
	if err := pending.Cancel("supplier withdrew", actor, now); err != nil {
		t.Fatalf("cancelling a DRAFT was refused: %v", err)
	}
	if pending.Status() != StatusCancelled || pending.Remarks() != "supplier withdrew" {
		t.Error("cancel did not record its reason")
	}

	committed, _ := approved(t, 5)
	if err := committed.Cancel("", actor, now); err != nil {
		t.Fatalf("cancelling an APPROVED was refused: %v", err)
	}
	if !hasEvent(committed, EventOrderCancelled) {
		t.Error("no cancelled event")
	}
}

// TestCancelIsRefusedOnceStockHasArrived protects inventory already booked
// against the order from being orphaned.
func TestCancelIsRefusedOnceStockHasArrived(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()
	if err := order.RecordReceipt(lineID, MustQuantity(1), actor, now); err != nil {
		t.Fatal(err)
	}

	if err := order.Cancel("", actor, now); err == nil {
		t.Fatal("a part-received order was cancelled, orphaning its inventory")
	}
	if order.Status() != StatusPartiallyReceived {
		t.Errorf("a refused cancel changed the status to %q", order.Status())
	}
}

// ---------- Downstream gates ----------

func TestCanGenerateASNAcrossTheLifecycle(t *testing.T) {
	actor, now := act()

	if draft(t).CanGenerateASN() {
		t.Error("a DRAFT order must not generate an ASN")
	}

	open, lineID := approved(t, 10)
	if !open.CanGenerateASN() {
		t.Error("an APPROVED order must be able to generate an ASN")
	}

	done, doneLine := approved(t, 2)
	if err := done.RecordReceipt(doneLine, MustQuantity(2), actor, now); err != nil {
		t.Fatal(err)
	}
	if done.CanGenerateASN() {
		t.Error("a COMPLETED order has nothing left to ship")
	}

	void, _ := approved(t, 5)
	if err := void.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}
	if void.CanGenerateASN() {
		t.Error("a CANCELLED order must not generate an ASN")
	}

	_ = lineID
}

func TestOnlyDraftsCanBeDeleted(t *testing.T) {
	if !draft(t).CanBeDeleted() {
		t.Error("a DRAFT should be deletable")
	}
	order, _ := approved(t, 5)
	if order.CanBeDeleted() {
		t.Error("an APPROVED order must be cancelled, not deleted")
	}
}

// ---------- Encapsulation ----------

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	order, lineID := approved(t, 10)
	actor, now := act()
	start := order.Version()

	_ = order.RecordReceipt(lineID, MustQuantity(3), actor, now)
	_ = order.RecordReceipt(lineID, MustQuantity(7), actor, now)

	if order.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d", order.Version(), start)
	}
}

func TestLinesAreDefensivelyCopied(t *testing.T) {
	order := draft(t)
	actor, now := act()
	supplied := []PurchaseOrderLine{mustLine(t, 5)}
	if err := order.ReplaceLines(supplied, actor, now); err != nil {
		t.Fatal(err)
	}

	supplied[0] = mustLine(t, 999)
	if order.Lines()[0].OrderedQty().Value() != 5 {
		t.Fatal("mutating the supplied slice changed the aggregate")
	}
	returned := order.Lines()
	returned[0] = mustLine(t, 111)
	if order.Lines()[0].OrderedQty().Value() != 5 {
		t.Fatal("mutating the returned slice changed the aggregate")
	}
}

func TestDuplicateProductLinesAreRefused(t *testing.T) {
	order := draft(t)
	actor, now := act()
	product := uuid.New()

	first, err := NewPurchaseOrderLine(uuid.New(), product, uuid.New(), MustQuantity(3), NoMoney(), "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPurchaseOrderLine(uuid.New(), product, uuid.New(), MustQuantity(4), NoMoney(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := order.ReplaceLines([]PurchaseOrderLine{first, second}, actor, now); err == nil {
		t.Error("the same product was accepted on two lines")
	}
}

// TestEmptyOrderIsNotFullyReceived guards the derivation: an order with no lines
// must not fall into COMPLETED.
func TestEmptyOrderIsNotFullyReceived(t *testing.T) {
	if draft(t).IsFullyReceived() {
		t.Error("an order with no lines reported as fully received")
	}
}
