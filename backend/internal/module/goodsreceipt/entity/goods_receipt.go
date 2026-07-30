package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// GoodsReceipt is the aggregate root for stock physically arriving at a
// warehouse.
//
// # Position in the inbound chain
//
//	PurchaseOrder -> ASN -> GoodsReceipt -> QualityInspection -> Putaway
//
// A receipt is the document that says "this arrived". It holds no stock: posting
// to inventory happens when the receipt is RECEIVED, and the application service
// performs that posting through the Inventory module. The aggregate's job is to
// decide WHETHER the posting may happen and to record that it did.
//
// # What it enforces
//
//	IsEditable   - only a DRAFT may be changed
//	Confirm      - a receipt with no lines cannot be checked off
//	Receive      - only a CONFIRMED receipt may post stock, exactly once
//
// RECEIVED is terminal. Once stock has been booked, editing or cancelling the
// paperwork would leave the document contradicting the ledger that witnessed the
// movement. Reversing a posted receipt is a new document, not an edit of this one.
//
// # Encapsulation
//
// Every field is unexported and there are no setters, so the lifecycle rules are
// not conventions a caller must remember — they are unreachable state.
type GoodsReceipt struct {
	id        uuid.UUID
	companyID uuid.UUID

	number ReceiptNumber

	warehouseID uuid.UUID

	// supplierID is optional: a transfer-in or a customer return arrives from no
	// supplier at all.
	supplierID *uuid.UUID

	reference DocumentReference

	receiptDate time.Time
	status      Status
	remarks     string

	lines []GoodsReceiptLine

	// version is the optimistic-lock token. READ-ONLY to the domain: no behaviour
	// below touches it. The repository is its sole owner.
	version uint64

	createdBy  uuid.UUID
	receivedBy *uuid.UUID
	updatedBy  uuid.UUID

	createdAt time.Time
	updatedAt time.Time

	events []Event
}

const maxRemarksLength = 1000

// NewGoodsReceipt opens a receipt in DRAFT.
func NewGoodsReceipt(
	id, companyID uuid.UUID,
	number ReceiptNumber,
	warehouseID uuid.UUID,
	supplierID *uuid.UUID,
	reference DocumentReference,
	receiptDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) (*GoodsReceipt, error) {
	const op = "goodsreceipt.entity.NewGoodsReceipt"

	if id == uuid.Nil {
		return nil, apperror.Validation("goods receipt id is required").WithOp(op)
	}
	if companyID == uuid.Nil {
		return nil, apperror.Validation("company is required").WithOp(op)
	}
	if actor == uuid.Nil {
		return nil, apperror.Validation("actor is required").WithOp(op)
	}
	if number.IsZero() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "required",
			Message: "goods receipt number is required",
		}).WithOp(op)
	}
	if warehouseID == uuid.Nil {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "warehouse_id", Rule: "required",
			Message: "receiving warehouse is required",
		}).WithOp(op)
	}
	if receiptDate.IsZero() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "receipt_date", Rule: "required",
			Message: "receipt date is required",
		}).WithOp(op)
	}
	if err := validateRemarks(remarks, op); err != nil {
		return nil, err
	}

	receipt := &GoodsReceipt{
		id:          id,
		companyID:   companyID,
		number:      number,
		warehouseID: warehouseID,
		supplierID:  copyID(supplierID),
		reference:   reference,
		receiptDate: receiptDate,
		status:      StatusDraft,
		remarks:     remarks,
		version:     1,
		createdBy:   actor,
		updatedBy:   actor,
		createdAt:   now,
		updatedAt:   now,
	}
	receipt.record(EventReceiptCreated, actor, now, map[string]any{
		"number":         number.String(),
		"warehouse_id":   warehouseID.String(),
		"reference_type": reference.Kind().String(),
	})
	return receipt, nil
}

// Reconstitute rebuilds a receipt from persisted state WITHOUT raising events.
//
// Loading a row is not a business event.
func Reconstitute(
	id, companyID uuid.UUID,
	number ReceiptNumber,
	warehouseID uuid.UUID,
	supplierID *uuid.UUID,
	reference DocumentReference,
	receiptDate time.Time,
	status Status,
	remarks string,
	lines []GoodsReceiptLine,
	version uint64,
	createdBy uuid.UUID,
	receivedBy *uuid.UUID,
	updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*GoodsReceipt, error) {
	const op = "goodsreceipt.entity.Reconstitute"

	if id == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("goods receipt identity is incomplete").WithOp(op)
	}
	if number.IsZero() {
		return nil, apperror.Validation("stored goods receipt has no number").WithOp(op)
	}
	if !status.Valid() {
		return nil, apperror.Validation("stored goods receipt has an invalid status").WithOp(op)
	}
	if version == 0 {
		return nil, apperror.Validation("stored goods receipt has version zero").WithOp(op)
	}

	restored := make([]GoodsReceiptLine, len(lines))
	copy(restored, lines)

	return &GoodsReceipt{
		id:          id,
		companyID:   companyID,
		number:      number,
		warehouseID: warehouseID,
		supplierID:  copyID(supplierID),
		reference:   reference,
		receiptDate: receiptDate,
		status:      status,
		remarks:     remarks,
		lines:       restored,
		version:     version,
		createdBy:   createdBy,
		receivedBy:  copyID(receivedBy),
		updatedBy:   updatedBy,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}, nil
}

// ---------------------------------------------------------------------------
// Behaviours — editing (DRAFT only)
// ---------------------------------------------------------------------------

// UpdateHeader replaces the editable header attributes. The NUMBER is not among
// them: it is the document's external reference.
func (g *GoodsReceipt) UpdateHeader(
	warehouseID uuid.UUID,
	supplierID *uuid.UUID,
	reference DocumentReference,
	receiptDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) error {
	const op = "goodsreceipt.entity.UpdateHeader"

	if err := g.requireEditable(op); err != nil {
		return err
	}
	if warehouseID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "warehouse_id", Rule: "required",
			Message: "receiving warehouse is required",
		}).WithOp(op)
	}
	if receiptDate.IsZero() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "receipt_date", Rule: "required",
			Message: "receipt date is required",
		}).WithOp(op)
	}
	if err := validateRemarks(remarks, op); err != nil {
		return err
	}

	g.warehouseID = warehouseID
	g.supplierID = copyID(supplierID)
	g.reference = reference
	g.receiptDate = receiptDate
	g.remarks = remarks
	g.touch(actor, now)
	g.record(EventReceiptUpdated, actor, now, map[string]any{
		"warehouse_id":   warehouseID.String(),
		"reference_type": reference.Kind().String(),
	})
	return nil
}

// ReplaceLines swaps the whole line set.
//
// Replacement rather than add/remove/edit: the lines are one value from the
// document's point of view, and a client editing a draft sends the complete
// desired state. That removes the partial-update ambiguity ("does an absent line
// mean unchanged, or deleted?") without losing any capability.
func (g *GoodsReceipt) ReplaceLines(lines []GoodsReceiptLine, actor uuid.UUID, now time.Time) error {
	const op = "goodsreceipt.entity.ReplaceLines"

	if err := g.requireEditable(op); err != nil {
		return err
	}
	if err := assertNoDuplicateStock(lines, op); err != nil {
		return err
	}

	replaced := make([]GoodsReceiptLine, len(lines))
	copy(replaced, lines)
	g.lines = replaced

	g.touch(actor, now)
	g.record(EventReceiptUpdated, actor, now, map[string]any{"line_count": len(replaced)})
	return nil
}

// ---------------------------------------------------------------------------
// Behaviours — lifecycle
// ---------------------------------------------------------------------------

// Confirm locks the document for posting.
//
// A receipt with no lines cannot be confirmed: confirming declares the delivery
// checked, and a receipt that records nothing arriving would then be postable,
// producing a "received" document that never touched stock.
func (g *GoodsReceipt) Confirm(actor uuid.UUID, now time.Time) error {
	const op = "goodsreceipt.entity.Confirm"

	if g.status != StatusDraft {
		return g.illegalTransition(StatusConfirmed, op)
	}
	if len(g.lines) == 0 {
		return apperror.NewValidation(apperror.FieldError{
			Field: "lines", Rule: "required",
			Message: "a goods receipt must have at least one line before it can be confirmed",
		}).WithOp(op)
	}

	g.status = StatusConfirmed
	g.touch(actor, now)
	g.record(EventReceiptConfirmed, actor, now, map[string]any{"line_count": len(g.lines)})
	return nil
}

// Receive marks the stock posted.
//
// Called by the application service AFTER the Inventory module has booked every
// line, inside the same transaction. The aggregate cannot verify that stock
// physically moved — but it can refuse to be received from any state other than
// CONFIRMED, which is what stops a draft from posting stock without ever being
// checked, and stops a posted receipt from posting a second time.
func (g *GoodsReceipt) Receive(actor uuid.UUID, now time.Time) error {
	const op = "goodsreceipt.entity.Receive"

	if g.status != StatusConfirmed {
		return g.illegalTransition(StatusReceived, op)
	}

	receiver := actor
	g.status = StatusReceived
	g.receivedBy = &receiver
	g.touch(actor, now)
	g.record(EventReceiptReceived, actor, now, map[string]any{
		"line_count":  len(g.lines),
		"total_units": g.TotalQuantity(),
	})
	return nil
}

// Cancel abandons the receipt.
//
// Permitted from DRAFT and CONFIRMED. NOT permitted from RECEIVED: the stock is
// already in inventory and the ledger has witnessed it, so cancelling the
// document would leave the paperwork denying a movement that demonstrably
// happened.
func (g *GoodsReceipt) Cancel(reason string, actor uuid.UUID, now time.Time) error {
	const op = "goodsreceipt.entity.Cancel"

	if g.status.IsTerminal() {
		return g.illegalTransition(StatusCancelled, op)
	}
	if err := validateRemarks(reason, op); err != nil {
		return err
	}

	g.status = StatusCancelled
	if reason != "" {
		g.remarks = reason
	}
	g.touch(actor, now)
	g.record(EventReceiptCancelled, actor, now, map[string]any{"reason": reason})
	return nil
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

func (g *GoodsReceipt) requireEditable(op string) error {
	if g.status.IsEditable() {
		return nil
	}
	return apperror.Conflict("a " + g.status.String() + " goods receipt cannot be edited").
		WithOp(op).
		WithDetails(map[string]any{"status": g.status.String()})
}

func (g *GoodsReceipt) illegalTransition(target Status, op string) error {
	return apperror.Conflict(
		"a " + g.status.String() + " goods receipt cannot become " + target.String()).
		WithOp(op).
		WithDetails(map[string]any{"status": g.status.String(), "target": target.String()})
}

func validateRemarks(remarks, op string) error {
	if len(remarks) > maxRemarksLength {
		return apperror.NewValidation(apperror.FieldError{
			Field: "remarks", Rule: "max",
			Message: "remarks must be at most 1000 characters",
		}).WithOp(op)
	}
	return nil
}

// assertNoDuplicateStock refuses two lines describing the same stock.
//
// Two lines for one product with the same lot and batch are almost always a
// double-entered row, and posting both would book twice what arrived. A caller
// receiving more units raises the quantity on one line. Serial-tracked lines are
// exempt: each names distinct physical items, so two such lines are never the
// same stock.
func assertNoDuplicateStock(lines []GoodsReceiptLine, op string) error {
	type stockKey struct {
		product    uuid.UUID
		batch, lot string
	}
	seen := make(map[stockKey]struct{}, len(lines))
	for _, line := range lines {
		if line.IsSerialTracked() {
			continue
		}
		key := stockKey{
			product: line.ProductID(),
			batch:   line.BatchNumber(),
			lot:     line.LotNumber(),
		}
		if _, duplicate := seen[key]; duplicate {
			return apperror.NewValidation(apperror.FieldError{
				Field: "lines", Rule: "duplicate",
				Message: "two lines describe the same product, batch and lot; combine them into one",
			}).WithOp(op)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// touch records who last changed the aggregate and when. It deliberately does
// NOT bump the version — that belongs to the repository.
func (g *GoodsReceipt) touch(actor uuid.UUID, now time.Time) {
	g.updatedBy = actor
	g.updatedAt = now
}

func copyID(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	copied := *id
	return &copied
}

// ---------------------------------------------------------------------------
// Readers
// ---------------------------------------------------------------------------

// ID identifies the receipt.
func (g *GoodsReceipt) ID() uuid.UUID { return g.id }

// CompanyID is the owning tenant.
func (g *GoodsReceipt) CompanyID() uuid.UUID { return g.companyID }

// Number is the operator-facing document number.
func (g *GoodsReceipt) Number() ReceiptNumber { return g.number }

// WarehouseID is where the goods arrived.
func (g *GoodsReceipt) WarehouseID() uuid.UUID { return g.warehouseID }

// SupplierID returns a COPY of the supplier, or nil when the stock came from none.
func (g *GoodsReceipt) SupplierID() *uuid.UUID { return copyID(g.supplierID) }

// Reference is the planning document this receipt was raised against.
func (g *GoodsReceipt) Reference() DocumentReference { return g.reference }

// ReceiptDate is the business date of the arrival.
func (g *GoodsReceipt) ReceiptDate() time.Time { return g.receiptDate }

// Status is the current lifecycle state.
func (g *GoodsReceipt) Status() Status { return g.status }

// Remarks is the free-text note.
func (g *GoodsReceipt) Remarks() string { return g.remarks }

// Lines returns a COPY of the line set.
func (g *GoodsReceipt) Lines() []GoodsReceiptLine {
	out := make([]GoodsReceiptLine, len(g.lines))
	copy(out, g.lines)
	return out
}

// LineCount reports how many lines the receipt carries.
func (g *GoodsReceipt) LineCount() int { return len(g.lines) }

// TotalQuantity sums the units across every line.
func (g *GoodsReceipt) TotalQuantity() int64 {
	var total int64
	for _, line := range g.lines {
		total += line.Quantity().Value()
	}
	return total
}

// IsEditable reports whether the document may still be changed.
func (g *GoodsReceipt) IsEditable() bool { return g.status.IsEditable() }

// IsReceived reports whether the stock has been posted.
func (g *GoodsReceipt) IsReceived() bool { return g.status == StatusReceived }

// Version is the optimistic-lock token.
func (g *GoodsReceipt) Version() uint64 { return g.version }

// BelongsTo reports whether the receipt is owned by the given tenant. It backs
// the defence-in-depth check in the service layer.
func (g *GoodsReceipt) BelongsTo(companyID uuid.UUID) bool { return g.companyID == companyID }

// CreatedBy is the actor who drafted the receipt.
func (g *GoodsReceipt) CreatedBy() uuid.UUID { return g.createdBy }

// ReceivedBy returns a COPY of the actor who posted the stock, or nil.
func (g *GoodsReceipt) ReceivedBy() *uuid.UUID { return copyID(g.receivedBy) }

// UpdatedBy is the actor who last changed it.
func (g *GoodsReceipt) UpdatedBy() uuid.UUID { return g.updatedBy }

// CreatedAt is when it was drafted.
func (g *GoodsReceipt) CreatedAt() time.Time { return g.createdAt }

// UpdatedAt is when it last changed.
func (g *GoodsReceipt) UpdatedAt() time.Time { return g.updatedAt }
