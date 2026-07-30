package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) {
	return uuid.New(), time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
}

func mustNumber(t *testing.T, raw string) ReceiptNumber {
	t.Helper()
	number, err := NewReceiptNumber(raw)
	if err != nil {
		t.Fatalf("NewReceiptNumber(%q) = %v", raw, err)
	}
	return number
}

func mustQuantity(t *testing.T, value int64) Quantity {
	t.Helper()
	quantity, err := NewQuantity(value)
	if err != nil {
		t.Fatalf("NewQuantity(%d) = %v", value, err)
	}
	return quantity
}

// mustLine builds an untracked line for a fresh product.
func mustLine(t *testing.T, quantity int64) GoodsReceiptLine {
	t.Helper()
	line, err := NewGoodsReceiptLine(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		mustQuantity(t, quantity), "", "", nil, nil, "",
	)
	if err != nil {
		t.Fatalf("NewGoodsReceiptLine = %v", err)
	}
	return line
}

// draft opens a DRAFT receipt and discards the creation event.
func draft(t *testing.T) *GoodsReceipt {
	t.Helper()
	actor, now := act()
	receipt, err := NewGoodsReceipt(
		uuid.New(), uuid.New(), mustNumber(t, "GR-001"), uuid.New(),
		nil, NoReference(), now, "", actor, now,
	)
	if err != nil {
		t.Fatalf("NewGoodsReceipt = %v", err)
	}
	receipt.PullEvents()
	return receipt
}

// confirmed returns a CONFIRMED receipt carrying one line.
func confirmed(t *testing.T) *GoodsReceipt {
	t.Helper()
	receipt := draft(t)
	actor, now := act()
	if err := receipt.ReplaceLines([]GoodsReceiptLine{mustLine(t, 10)}, actor, now); err != nil {
		t.Fatalf("ReplaceLines = %v", err)
	}
	if err := receipt.Confirm(actor, now); err != nil {
		t.Fatalf("Confirm = %v", err)
	}
	receipt.PullEvents()
	return receipt
}

func hasEvent(receipt *GoodsReceipt, name EventName) bool {
	for _, event := range receipt.PullEvents() {
		if event.Name == name {
			return true
		}
	}
	return false
}

// ---------- Factory ----------

func TestNewGoodsReceiptStartsDraft(t *testing.T) {
	actor, now := act()
	company := uuid.New()

	receipt, err := NewGoodsReceipt(
		uuid.New(), company, mustNumber(t, "gr-001"), uuid.New(),
		nil, NoReference(), now, "dock 3", actor, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status() != StatusDraft || !receipt.IsEditable() {
		t.Errorf("status = %q, want an editable DRAFT", receipt.Status())
	}
	if receipt.Version() != 1 || receipt.LineCount() != 0 {
		t.Error("a new receipt should be version 1 with no lines")
	}
	if receipt.Number().String() != "GR-001" {
		t.Errorf("number = %q, want canonicalised GR-001", receipt.Number())
	}
	if receipt.ReceivedBy() != nil || receipt.IsReceived() {
		t.Error("a draft must not carry receipt metadata")
	}
	if !receipt.BelongsTo(company) {
		t.Error("receipt does not belong to its own company")
	}
	events := receipt.PullEvents()
	if len(events) != 1 || events[0].Name != EventReceiptCreated {
		t.Fatalf("expected exactly one created event, got %+v", events)
	}
}

func TestNewGoodsReceiptRejectsBadInput(t *testing.T) {
	actor, now := act()
	number := mustNumber(t, "GR-002")
	warehouse := uuid.New()

	cases := map[string]func() error{
		"nil id": func() error {
			_, err := NewGoodsReceipt(uuid.Nil, uuid.New(), number, warehouse, nil, NoReference(), now, "", actor, now)
			return err
		},
		"nil company": func() error {
			_, err := NewGoodsReceipt(uuid.New(), uuid.Nil, number, warehouse, nil, NoReference(), now, "", actor, now)
			return err
		},
		"nil actor": func() error {
			_, err := NewGoodsReceipt(uuid.New(), uuid.New(), number, warehouse, nil, NoReference(), now, "", uuid.Nil, now)
			return err
		},
		"empty number": func() error {
			_, err := NewGoodsReceipt(uuid.New(), uuid.New(), ReceiptNumber{}, warehouse, nil, NoReference(), now, "", actor, now)
			return err
		},
		"nil warehouse": func() error {
			_, err := NewGoodsReceipt(uuid.New(), uuid.New(), number, uuid.Nil, nil, NoReference(), now, "", actor, now)
			return err
		},
		"zero receipt date": func() error {
			_, err := NewGoodsReceipt(uuid.New(), uuid.New(), number, warehouse, nil, NoReference(), time.Time{}, "", actor, now)
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
	receiver := uuid.New()
	receipt, err := Reconstitute(
		uuid.New(), uuid.New(), mustNumber(t, "GR-003"), uuid.New(),
		nil, NoReference(), now, StatusReceived, "",
		[]GoodsReceiptLine{mustLine(t, 5)}, 3, actor, &receiver, actor, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := receipt.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if receipt.Version() != 3 || !receipt.IsReceived() || receipt.LineCount() != 1 {
		t.Error("reconstituted state wrong")
	}
}

// ---------- Editability ----------

func TestConfirmedReceiptIsLocked(t *testing.T) {
	receipt := confirmed(t)
	actor, now := act()

	if err := receipt.ReplaceLines([]GoodsReceiptLine{mustLine(t, 1)}, actor, now); err == nil {
		t.Error("lines were replaced on a CONFIRMED receipt")
	}
	if err := receipt.UpdateHeader(uuid.New(), nil, NoReference(), now, "x", actor, now); err == nil {
		t.Error("the header was edited on a CONFIRMED receipt")
	}
	if receipt.LineCount() != 1 {
		t.Errorf("a refused edit changed the line count to %d", receipt.LineCount())
	}
}

func TestTerminalReceiptsAreLocked(t *testing.T) {
	actor, now := act()

	received := confirmed(t)
	if err := received.Receive(actor, now); err != nil {
		t.Fatal(err)
	}
	cancelled := confirmed(t)
	if err := cancelled.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}

	for label, receipt := range map[string]*GoodsReceipt{"RECEIVED": received, "CANCELLED": cancelled} {
		if receipt.IsEditable() {
			t.Errorf("%s should not be editable", label)
		}
		if err := receipt.ReplaceLines(nil, actor, now); err == nil {
			t.Errorf("%s accepted a line replacement", label)
		}
	}
}

// ---------- Lifecycle ----------

func TestConfirmRequiresAtLeastOneLine(t *testing.T) {
	receipt := draft(t)
	actor, now := act()

	if err := receipt.Confirm(actor, now); err == nil {
		t.Fatal("an empty receipt was confirmed")
	}
	if receipt.Status() != StatusDraft {
		t.Errorf("a refused confirm changed the status to %q", receipt.Status())
	}
}

func TestReceiveRecordsReceiverAndIsTerminal(t *testing.T) {
	receipt := confirmed(t)
	actor, now := act()

	if err := receipt.Receive(actor, now); err != nil {
		t.Fatal(err)
	}
	if !receipt.IsReceived() {
		t.Fatalf("status = %q, want RECEIVED", receipt.Status())
	}
	if receipt.ReceivedBy() == nil || *receipt.ReceivedBy() != actor {
		t.Error("receiver not recorded")
	}
	if !hasEvent(receipt, EventReceiptReceived) {
		t.Error("no received event")
	}
	// Posting twice would create the stock twice.
	if err := receipt.Receive(actor, now); err == nil {
		t.Error("a RECEIVED receipt was received again")
	}
}

// TestDraftCannotPostStock is the rule that stops an unchecked delivery from
// creating inventory.
func TestDraftCannotPostStock(t *testing.T) {
	receipt := draft(t)
	actor, now := act()
	if err := receipt.ReplaceLines([]GoodsReceiptLine{mustLine(t, 5)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Receive(actor, now); err == nil {
		t.Fatal("a DRAFT receipt posted stock without ever being confirmed")
	}
	if receipt.ReceivedBy() != nil {
		t.Error("a refused receive recorded a receiver")
	}
}

// TestReceivedReceiptCannotBeCancelled protects the ledger from being
// contradicted by its own paperwork.
func TestReceivedReceiptCannotBeCancelled(t *testing.T) {
	receipt := confirmed(t)
	actor, now := act()
	if err := receipt.Receive(actor, now); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Cancel("mistake", actor, now); err == nil {
		t.Fatal("a RECEIVED receipt was cancelled, denying a movement that happened")
	}
}

func TestCancelIsAllowedFromDraftAndConfirmed(t *testing.T) {
	actor, now := act()

	pending := draft(t)
	if err := pending.Cancel("truck never came", actor, now); err != nil {
		t.Fatalf("cancelling a DRAFT was refused: %v", err)
	}
	if pending.Status() != StatusCancelled || pending.Remarks() != "truck never came" {
		t.Error("cancel did not record its reason")
	}

	checked := confirmed(t)
	if err := checked.Cancel("", actor, now); err != nil {
		t.Fatalf("cancelling a CONFIRMED was refused: %v", err)
	}
	if !hasEvent(checked, EventReceiptCancelled) {
		t.Error("no cancelled event")
	}
}

// ---------- Encapsulation ----------

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	receipt := draft(t)
	actor, now := act()
	start := receipt.Version()

	_ = receipt.ReplaceLines([]GoodsReceiptLine{mustLine(t, 2)}, actor, now)
	_ = receipt.Confirm(actor, now)
	_ = receipt.Receive(actor, now)

	if receipt.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d", receipt.Version(), start)
	}
}

func TestLinesAreDefensivelyCopied(t *testing.T) {
	receipt := draft(t)
	actor, now := act()
	supplied := []GoodsReceiptLine{mustLine(t, 5)}
	if err := receipt.ReplaceLines(supplied, actor, now); err != nil {
		t.Fatal(err)
	}

	supplied[0] = mustLine(t, 999)
	if receipt.Lines()[0].Quantity().Value() != 5 {
		t.Fatal("mutating the supplied slice changed the aggregate")
	}
	returned := receipt.Lines()
	returned[0] = mustLine(t, 111)
	if receipt.Lines()[0].Quantity().Value() != 5 {
		t.Fatal("mutating the returned slice changed the aggregate")
	}
}

func TestDuplicateStockLinesAreRefused(t *testing.T) {
	receipt := draft(t)
	actor, now := act()
	product, location, uom := uuid.New(), uuid.New(), uuid.New()

	first, err := NewGoodsReceiptLine(uuid.New(), product, location, uom, mustQuantity(t, 3), "B1", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGoodsReceiptLine(uuid.New(), product, location, uom, mustQuantity(t, 4), "B1", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.ReplaceLines([]GoodsReceiptLine{first, second}, actor, now); err == nil {
		t.Error("two lines for the same product/batch/lot were accepted")
	}

	// A different batch is legitimately a different consignment.
	other, err := NewGoodsReceiptLine(uuid.New(), product, location, uom, mustQuantity(t, 4), "B2", "", nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := receipt.ReplaceLines([]GoodsReceiptLine{first, other}, actor, now); err != nil {
		t.Errorf("two batches of one product were refused: %v", err)
	}
}

func TestTotalQuantitySums(t *testing.T) {
	receipt := draft(t)
	actor, now := act()
	if err := receipt.ReplaceLines([]GoodsReceiptLine{mustLine(t, 3), mustLine(t, 7)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if receipt.TotalQuantity() != 10 {
		t.Errorf("total = %d, want 10", receipt.TotalQuantity())
	}
}
