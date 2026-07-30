package entity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) {
	return uuid.New(), time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
}

func mustNumber(t *testing.T, raw string) TransferNumber {
	t.Helper()
	number, err := NewTransferNumber(raw)
	if err != nil {
		t.Fatalf("NewTransferNumber(%q) = %v", raw, err)
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

// mustLine builds a valid untracked line between two fresh locations.
func mustLine(t *testing.T, quantity int64) StockTransferLine {
	t.Helper()
	line, err := NewStockTransferLine(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		mustQuantity(t, quantity), NoLineAttributes(),
	)
	if err != nil {
		t.Fatalf("NewStockTransferLine = %v", err)
	}
	return line
}

// build opens a DRAFT transfer and discards the creation event.
func build(t *testing.T) *StockTransfer {
	t.Helper()
	actor, now := act()
	transfer, err := NewStockTransfer(
		uuid.New(), uuid.New(), mustNumber(t, "ST-001"),
		uuid.New(), uuid.New(), now, "", actor, now,
	)
	if err != nil {
		t.Fatalf("NewStockTransfer = %v", err)
	}
	transfer.PullEvents()
	return transfer
}

// confirmed returns a CONFIRMED transfer carrying one line.
func confirmed(t *testing.T) *StockTransfer {
	t.Helper()
	transfer := build(t)
	actor, now := act()
	if err := transfer.ReplaceLines([]StockTransferLine{mustLine(t, 5)}, actor, now); err != nil {
		t.Fatalf("ReplaceLines = %v", err)
	}
	if err := transfer.Confirm(actor, now); err != nil {
		t.Fatalf("Confirm = %v", err)
	}
	transfer.PullEvents()
	return transfer
}

func hasEvent(transfer *StockTransfer, name EventName) bool {
	for _, event := range transfer.PullEvents() {
		if event.Name == name {
			return true
		}
	}
	return false
}

// ---------- Factory ----------

func TestNewStockTransferStartsDraft(t *testing.T) {
	actor, now := act()
	company, from, to := uuid.New(), uuid.New(), uuid.New()

	transfer, err := NewStockTransfer(
		uuid.New(), company, mustNumber(t, "st-001"), from, to, now, "urgent", actor, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Status() != StatusDraft || !transfer.IsEditable() {
		t.Errorf("status = %q, want an editable DRAFT", transfer.Status())
	}
	if transfer.Version() != 1 {
		t.Errorf("version = %d, want 1", transfer.Version())
	}
	if transfer.Number().String() != "ST-001" {
		t.Errorf("number = %q, want canonicalised ST-001", transfer.Number())
	}
	if transfer.LineCount() != 0 {
		t.Error("a new transfer should open with no lines")
	}
	if !transfer.BelongsTo(company) {
		t.Error("transfer does not belong to its own company")
	}
	events := transfer.PullEvents()
	if len(events) != 1 || events[0].Name != EventTransferCreated {
		t.Fatalf("expected exactly one created event, got %+v", events)
	}
}

// TestSameWarehouseTransferIsAllowed pins the commonest real case: moving a
// pallet between bins inside one building.
func TestSameWarehouseTransferIsAllowed(t *testing.T) {
	actor, now := act()
	warehouse := uuid.New()

	transfer, err := NewStockTransfer(
		uuid.New(), uuid.New(), mustNumber(t, "ST-002"), warehouse, warehouse, now, "", actor, now,
	)
	if err != nil {
		t.Fatalf("a same-warehouse transfer was rejected: %v", err)
	}
	if !transfer.IsSameWarehouse() {
		t.Error("IsSameWarehouse should report true")
	}
}

func TestCrossWarehouseTransferIsAllowed(t *testing.T) {
	transfer := build(t)
	if transfer.IsSameWarehouse() {
		t.Error("two distinct warehouses should not report as the same site")
	}
}

func TestNewStockTransferRejectsBadInput(t *testing.T) {
	actor, now := act()
	number := mustNumber(t, "ST-003")
	from, to := uuid.New(), uuid.New()

	cases := map[string]func() error{
		"nil id": func() error {
			_, err := NewStockTransfer(uuid.Nil, uuid.New(), number, from, to, now, "", actor, now)
			return err
		},
		"nil company": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.Nil, number, from, to, now, "", actor, now)
			return err
		},
		"nil actor": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), number, from, to, now, "", uuid.Nil, now)
			return err
		},
		"empty number": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), TransferNumber{}, from, to, now, "", actor, now)
			return err
		},
		"nil source warehouse": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), number, uuid.Nil, to, now, "", actor, now)
			return err
		},
		"nil destination warehouse": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), number, from, uuid.Nil, now, "", actor, now)
			return err
		},
		"zero transfer date": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), number, from, to, time.Time{}, "", actor, now)
			return err
		},
		"over-long remarks": func() error {
			_, err := NewStockTransfer(uuid.New(), uuid.New(), number, from, to, now,
				strings.Repeat("x", maxRemarksLength+1), actor, now)
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
	transfer, err := Reconstitute(
		uuid.New(), uuid.New(), mustNumber(t, "ST-004"), uuid.New(), uuid.New(),
		StatusCompleted, now, "", []StockTransferLine{mustLine(t, 3)},
		7, actor, actor, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := transfer.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if transfer.Version() != 7 || transfer.Status() != StatusCompleted || transfer.LineCount() != 1 {
		t.Error("reconstituted state wrong")
	}
}

func TestReconstituteRejectsCorruptRows(t *testing.T) {
	actor, now := act()
	number := mustNumber(t, "ST-005")
	from, to := uuid.New(), uuid.New()

	cases := map[string]func() error{
		"version zero": func() error {
			_, err := Reconstitute(uuid.New(), uuid.New(), number, from, to, StatusDraft, now, "", nil, 0, actor, actor, now, now)
			return err
		},
		"invalid status": func() error {
			_, err := Reconstitute(uuid.New(), uuid.New(), number, from, to, Status("X"), now, "", nil, 1, actor, actor, now, now)
			return err
		},
		"nil company": func() error {
			_, err := Reconstitute(uuid.New(), uuid.Nil, number, from, to, StatusDraft, now, "", nil, 1, actor, actor, now, now)
			return err
		},
		"missing number": func() error {
			_, err := Reconstitute(uuid.New(), uuid.New(), TransferNumber{}, from, to, StatusDraft, now, "", nil, 1, actor, actor, now, now)
			return err
		},
	}
	for label, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

// ---------- Editability ----------

// TestConfirmedTransferIsLocked is the module's central rule: once approved, the
// document's contents are unreachable.
func TestConfirmedTransferIsLocked(t *testing.T) {
	transfer := confirmed(t)
	actor, now := act()

	if err := transfer.ReplaceLines([]StockTransferLine{mustLine(t, 9)}, actor, now); err == nil {
		t.Error("lines were replaced on a CONFIRMED transfer")
	}
	if err := transfer.UpdateHeader(uuid.New(), uuid.New(), now, "changed", actor, now); err == nil {
		t.Error("the header was edited on a CONFIRMED transfer")
	}
	if transfer.LineCount() != 1 {
		t.Errorf("a refused edit changed the line count to %d", transfer.LineCount())
	}
}

func TestTerminalTransfersAreLocked(t *testing.T) {
	actor, now := act()
	for _, terminal := range []Status{StatusCompleted, StatusCancelled} {
		transfer := confirmed(t)
		switch terminal {
		case StatusCompleted:
			if err := transfer.Complete(actor, now); err != nil {
				t.Fatal(err)
			}
		case StatusCancelled:
			if err := transfer.Cancel("", actor, now); err != nil {
				t.Fatal(err)
			}
		}
		if transfer.IsEditable() {
			t.Errorf("%s should not be editable", terminal)
		}
		if err := transfer.ReplaceLines(nil, actor, now); err == nil {
			t.Errorf("%s transfer accepted a line replacement", terminal)
		}
	}
}

func TestDraftAcceptsHeaderAndLineEdits(t *testing.T) {
	transfer := build(t)
	actor, now := act()
	from, to := uuid.New(), uuid.New()

	if err := transfer.UpdateHeader(from, to, now, "revised", actor, now); err != nil {
		t.Fatal(err)
	}
	if transfer.FromWarehouseID() != from || transfer.ToWarehouseID() != to || transfer.Remarks() != "revised" {
		t.Error("header edit did not apply")
	}
	if !hasEvent(transfer, EventTransferUpdated) {
		t.Error("no updated event after a header edit")
	}

	if err := transfer.ReplaceLines([]StockTransferLine{mustLine(t, 1), mustLine(t, 2)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if transfer.LineCount() != 2 {
		t.Errorf("line count = %d, want 2", transfer.LineCount())
	}
}

// TestUpdateHeaderCannotChangeTheNumber pins that the document number is not an
// editable attribute — there is no parameter for it.
func TestUpdateHeaderCannotChangeTheNumber(t *testing.T) {
	transfer := build(t)
	actor, now := act()
	before := transfer.Number().String()

	if err := transfer.UpdateHeader(uuid.New(), uuid.New(), now, "", actor, now); err != nil {
		t.Fatal(err)
	}
	if transfer.Number().String() != before {
		t.Errorf("number changed from %q to %q", before, transfer.Number())
	}
}

// ---------- Lifecycle ----------

func TestConfirmRequiresAtLeastOneLine(t *testing.T) {
	transfer := build(t)
	actor, now := act()

	if err := transfer.Confirm(actor, now); err == nil {
		t.Fatal("an empty transfer was confirmed")
	}
	if transfer.Status() != StatusDraft {
		t.Errorf("a refused confirm changed the status to %q", transfer.Status())
	}
}

func TestLifecycleHappyPath(t *testing.T) {
	transfer := build(t)
	actor, now := act()

	if err := transfer.ReplaceLines([]StockTransferLine{mustLine(t, 4)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if err := transfer.Confirm(actor, now); err != nil {
		t.Fatal(err)
	}
	if transfer.Status() != StatusConfirmed || !hasEvent(transfer, EventTransferConfirmed) {
		t.Fatal("confirm did not take effect")
	}
	if err := transfer.Complete(actor, now); err != nil {
		t.Fatal(err)
	}
	if transfer.Status() != StatusCompleted || !hasEvent(transfer, EventTransferCompleted) {
		t.Fatal("complete did not take effect")
	}
}

// TestIllegalTransitionsAreRefused walks every transition the state machine must
// reject, so a future edit to the guards cannot quietly open one.
func TestIllegalTransitionsAreRefused(t *testing.T) {
	actor, now := act()

	// A draft cannot be completed without being confirmed first.
	draft := build(t)
	if err := draft.ReplaceLines([]StockTransferLine{mustLine(t, 1)}, actor, now); err != nil {
		t.Fatal(err)
	}
	if err := draft.Complete(actor, now); err == nil {
		t.Error("a DRAFT transfer was completed")
	}

	// A confirmed transfer cannot be confirmed twice.
	twice := confirmed(t)
	if err := twice.Confirm(actor, now); err == nil {
		t.Error("a CONFIRMED transfer was confirmed again")
	}

	// A completed transfer is terminal in every direction.
	done := confirmed(t)
	if err := done.Complete(actor, now); err != nil {
		t.Fatal(err)
	}
	if err := done.Cancel("changed my mind", actor, now); err == nil {
		t.Error("a COMPLETED transfer was cancelled — the document would contradict the ledger")
	}
	if err := done.Complete(actor, now); err == nil {
		t.Error("a COMPLETED transfer was completed twice")
	}

	// A cancelled transfer cannot be revived.
	void := confirmed(t)
	if err := void.Cancel("", actor, now); err != nil {
		t.Fatal(err)
	}
	if err := void.Confirm(actor, now); err == nil {
		t.Error("a CANCELLED transfer was confirmed")
	}
	if err := void.Complete(actor, now); err == nil {
		t.Error("a CANCELLED transfer was completed")
	}
}

func TestCancelIsAllowedFromDraftAndConfirmed(t *testing.T) {
	actor, now := act()

	draft := build(t)
	if err := draft.Cancel("no longer needed", actor, now); err != nil {
		t.Fatalf("cancelling a DRAFT was refused: %v", err)
	}
	if draft.Status() != StatusCancelled || draft.Remarks() != "no longer needed" {
		t.Error("cancel did not record its reason")
	}

	approved := confirmed(t)
	if err := approved.Cancel("", actor, now); err != nil {
		t.Fatalf("cancelling a CONFIRMED was refused: %v", err)
	}
	if approved.Status() != StatusCancelled || !hasEvent(approved, EventTransferCancelled) {
		t.Error("cancel did not take effect")
	}
}

// ---------- Encapsulation ----------

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	transfer := build(t)
	actor, now := act()
	start := transfer.Version()

	_ = transfer.UpdateHeader(uuid.New(), uuid.New(), now, "x", actor, now)
	_ = transfer.ReplaceLines([]StockTransferLine{mustLine(t, 1)}, actor, now)
	_ = transfer.Confirm(actor, now)
	_ = transfer.Complete(actor, now)

	if transfer.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d", transfer.Version(), start)
	}
}

// TestLinesAreDefensivelyCopied proves a caller holding the returned slice
// cannot rewrite the aggregate's lines.
func TestLinesAreDefensivelyCopied(t *testing.T) {
	transfer := build(t)
	actor, now := act()
	original := mustLine(t, 5)
	if err := transfer.ReplaceLines([]StockTransferLine{original}, actor, now); err != nil {
		t.Fatal(err)
	}

	returned := transfer.Lines()
	returned[0] = mustLine(t, 999)
	if transfer.Lines()[0].Quantity().Value() != 5 {
		t.Fatal("mutating the returned slice changed the aggregate's lines")
	}

	// The slice PASSED IN is copied too.
	supplied := []StockTransferLine{mustLine(t, 7)}
	draft := build(t)
	if err := draft.ReplaceLines(supplied, actor, now); err != nil {
		t.Fatal(err)
	}
	supplied[0] = mustLine(t, 111)
	if draft.Lines()[0].Quantity().Value() != 7 {
		t.Fatal("mutating the supplied slice changed the aggregate's lines")
	}
}

func TestDuplicateMovementLinesAreRefused(t *testing.T) {
	transfer := build(t)
	actor, now := act()
	product, from, to := uuid.New(), uuid.New(), uuid.New()

	first, err := NewStockTransferLine(uuid.New(), product, from, to, mustQuantity(t, 3), NoLineAttributes())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStockTransferLine(uuid.New(), product, from, to, mustQuantity(t, 4), NoLineAttributes())
	if err != nil {
		t.Fatal(err)
	}

	if err := transfer.ReplaceLines([]StockTransferLine{first, second}, actor, now); err == nil {
		t.Error("two lines moving the same product along the same route were accepted")
	}

	// The same product on a DIFFERENT route is legitimate.
	elsewhere, err := NewStockTransferLine(uuid.New(), product, from, uuid.New(), mustQuantity(t, 4), NoLineAttributes())
	if err != nil {
		t.Fatal(err)
	}
	if err := transfer.ReplaceLines([]StockTransferLine{first, elsewhere}, actor, now); err != nil {
		t.Errorf("two routes for one product were refused: %v", err)
	}
}
