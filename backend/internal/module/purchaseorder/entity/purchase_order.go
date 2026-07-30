package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// PurchaseOrder is the aggregate root for planned inbound inventory.
//
// # What it is
//
// A purchase order is the PLANNING document of the inbound chain. It states what
// a company has committed to buy, from whom, into which warehouse, and by when.
// It holds no stock and changes no balance — receiving does that, and receiving
// happens downstream:
//
//	PurchaseOrder  ->  ASN  ->  GoodsReceipt  ->  QualityInspection  ->  Putaway
//
// Each step references the one before it. That chain is why a Goods Receipt must
// never be raised straight from a Supplier when an order exists: doing so would
// book stock against no commitment, and the order would still show its full
// quantity outstanding forever.
//
// # What it enforces
//
// The aggregate owns three rules that nothing downstream may reinterpret:
//
//	IsEditable        - only a DRAFT may be changed at all
//	IsOpenForReceipt  - only an APPROVED or PARTIALLY_RECEIVED order may be
//	                    received against, which is what stops a receipt from
//	                    referencing a draft
//	CanGenerateASN    - a cancelled or completed order produces no new shipment
//
// # Encapsulation
//
// Every field is unexported and there are no setters. PARTIALLY_RECEIVED and
// COMPLETED are DERIVED inside RecordReceipt from the line quantities; there is
// no method that sets them, so the status can never disagree with the lines it
// summarises.
type PurchaseOrder struct {
	id        uuid.UUID
	companyID uuid.UUID

	number OrderNumber

	supplierID  uuid.UUID
	warehouseID uuid.UUID

	orderDate           time.Time
	expectedArrivalDate time.Time

	status  Status
	remarks string

	lines []PurchaseOrderLine

	// version is the optimistic-lock token. READ-ONLY to the domain: no behaviour
	// below touches it. The repository is its sole owner, because the version
	// tracks a ROW's revision, which is a persistence fact.
	version uint64

	createdBy  uuid.UUID
	approvedBy *uuid.UUID
	approvedAt *time.Time

	updatedBy uuid.UUID
	createdAt time.Time
	updatedAt time.Time

	events []Event
}

const maxRemarksLength = 1000

// NewPurchaseOrder opens an order in DRAFT.
//
// An order starts empty and editable. Lines are added afterwards rather than
// passed here, because a document being drafted legitimately has no lines yet and
// forcing a caller to supply them would make "save my work in progress"
// impossible.
func NewPurchaseOrder(
	id, companyID uuid.UUID,
	number OrderNumber,
	supplierID, warehouseID uuid.UUID,
	orderDate, expectedArrivalDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) (*PurchaseOrder, error) {
	const op = "purchaseorder.entity.NewPurchaseOrder"

	if id == uuid.Nil {
		return nil, apperror.Validation("purchase order id is required").WithOp(op)
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
			Message: "purchase order number is required",
		}).WithOp(op)
	}
	if err := validateParties(supplierID, warehouseID, op); err != nil {
		return nil, err
	}
	if err := validateDates(orderDate, expectedArrivalDate, op); err != nil {
		return nil, err
	}
	if err := validateRemarks(remarks, op); err != nil {
		return nil, err
	}

	order := &PurchaseOrder{
		id:                  id,
		companyID:           companyID,
		number:              number,
		supplierID:          supplierID,
		warehouseID:         warehouseID,
		orderDate:           orderDate,
		expectedArrivalDate: expectedArrivalDate,
		status:              StatusDraft,
		remarks:             remarks,
		version:             1,
		createdBy:           actor,
		updatedBy:           actor,
		createdAt:           now,
		updatedAt:           now,
	}
	order.record(EventOrderCreated, actor, now, map[string]any{
		"number":       number.String(),
		"supplier_id":  supplierID.String(),
		"warehouse_id": warehouseID.String(),
	})
	return order, nil
}

// Reconstitute rebuilds an order from persisted state WITHOUT raising events.
//
// Loading a row is not a business event. If this raised the creation event, every
// read would re-announce an order that was created once, long ago.
func Reconstitute(
	id, companyID uuid.UUID,
	number OrderNumber,
	supplierID, warehouseID uuid.UUID,
	orderDate, expectedArrivalDate time.Time,
	status Status,
	remarks string,
	lines []PurchaseOrderLine,
	version uint64,
	createdBy uuid.UUID,
	approvedBy *uuid.UUID,
	approvedAt *time.Time,
	updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*PurchaseOrder, error) {
	const op = "purchaseorder.entity.Reconstitute"

	if id == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("purchase order identity is incomplete").WithOp(op)
	}
	if number.IsZero() {
		return nil, apperror.Validation("stored purchase order has no number").WithOp(op)
	}
	if !status.Valid() {
		return nil, apperror.Validation("stored purchase order has an invalid status").WithOp(op)
	}
	if version == 0 {
		return nil, apperror.Validation("stored purchase order has version zero").WithOp(op)
	}
	if err := validateParties(supplierID, warehouseID, op); err != nil {
		return nil, err
	}

	restored := make([]PurchaseOrderLine, len(lines))
	copy(restored, lines)

	return &PurchaseOrder{
		id:                  id,
		companyID:           companyID,
		number:              number,
		supplierID:          supplierID,
		warehouseID:         warehouseID,
		orderDate:           orderDate,
		expectedArrivalDate: expectedArrivalDate,
		status:              status,
		remarks:             remarks,
		lines:               restored,
		version:             version,
		createdBy:           createdBy,
		approvedBy:          copyID(approvedBy),
		approvedAt:          copyTime(approvedAt),
		updatedBy:           updatedBy,
		createdAt:           createdAt,
		updatedAt:           updatedAt,
	}, nil
}

// ---------------------------------------------------------------------------
// Behaviours — editing (DRAFT only)
// ---------------------------------------------------------------------------

// UpdateHeader replaces the editable header attributes.
//
// The NUMBER is not among them: it is printed on the document the supplier
// receives, and renaming it would strand every reference to it.
func (o *PurchaseOrder) UpdateHeader(
	supplierID, warehouseID uuid.UUID,
	orderDate, expectedArrivalDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) error {
	const op = "purchaseorder.entity.UpdateHeader"

	if err := o.requireEditable(op); err != nil {
		return err
	}
	if err := validateParties(supplierID, warehouseID, op); err != nil {
		return err
	}
	if err := validateDates(orderDate, expectedArrivalDate, op); err != nil {
		return err
	}
	if err := validateRemarks(remarks, op); err != nil {
		return err
	}

	o.supplierID = supplierID
	o.warehouseID = warehouseID
	o.orderDate = orderDate
	o.expectedArrivalDate = expectedArrivalDate
	o.remarks = remarks
	o.touch(actor, now)
	o.record(EventOrderUpdated, actor, now, map[string]any{
		"supplier_id":  supplierID.String(),
		"warehouse_id": warehouseID.String(),
	})
	return nil
}

// ReplaceLines swaps the whole line set.
//
// Replacement rather than add/remove/edit: the lines are a single value from the
// document's point of view, and a client editing a draft sends the complete
// desired state. That removes a class of partial-update ambiguity ("does an
// absent line mean unchanged, or deleted?") without losing any capability.
//
// It is DRAFT-only, so a received quantity can never be discarded by an edit —
// by the time anything has been received the order is past DRAFT.
func (o *PurchaseOrder) ReplaceLines(lines []PurchaseOrderLine, actor uuid.UUID, now time.Time) error {
	const op = "purchaseorder.entity.ReplaceLines"

	if err := o.requireEditable(op); err != nil {
		return err
	}
	if err := assertNoDuplicateProducts(lines, op); err != nil {
		return err
	}

	replaced := make([]PurchaseOrderLine, len(lines))
	copy(replaced, lines)
	o.lines = replaced

	o.touch(actor, now)
	o.record(EventOrderUpdated, actor, now, map[string]any{"line_count": len(replaced)})
	return nil
}

// ---------------------------------------------------------------------------
// Behaviours — lifecycle
// ---------------------------------------------------------------------------

// Approve commits the order and unlocks the inbound chain.
//
// An order with no lines cannot be approved: approving commits the company to buy
// something, and an empty order commits it to nothing while becoming eligible to
// generate an ASN that would carry no goods.
func (o *PurchaseOrder) Approve(actor uuid.UUID, now time.Time) error {
	const op = "purchaseorder.entity.Approve"

	if o.status != StatusDraft {
		return o.illegalTransition(StatusApproved, op)
	}
	if len(o.lines) == 0 {
		return apperror.NewValidation(apperror.FieldError{
			Field: "lines", Rule: "required",
			Message: "a purchase order must have at least one line before it can be approved",
		}).WithOp(op)
	}

	approver, at := actor, now
	o.status = StatusApproved
	o.approvedBy = &approver
	o.approvedAt = &at
	o.touch(actor, now)
	o.record(EventOrderApproved, actor, now, map[string]any{"line_count": len(o.lines)})
	return nil
}

// Cancel calls the order off.
//
// Permitted from DRAFT and APPROVED. It is REFUSED once any quantity has been
// received, which is why PARTIALLY_RECEIVED is not in the allowed set: stock has
// physically arrived and been booked against this order, and cancelling the
// document would orphan that inventory. Closing a part-delivered order short is a
// different operation, and it belongs to the sprint that implements it.
func (o *PurchaseOrder) Cancel(reason string, actor uuid.UUID, now time.Time) error {
	const op = "purchaseorder.entity.Cancel"

	if o.status != StatusDraft && o.status != StatusApproved {
		return o.illegalTransition(StatusCancelled, op)
	}
	if o.HasReceipts() {
		return apperror.Conflict("a purchase order with received quantity cannot be cancelled").
			WithOp(op).
			WithDetails(map[string]any{"received_lines": o.receivedLineCount()})
	}
	if err := validateRemarks(reason, op); err != nil {
		return err
	}

	o.status = StatusCancelled
	if reason != "" {
		o.remarks = reason
	}
	o.touch(actor, now)
	o.record(EventOrderCancelled, actor, now, map[string]any{"reason": reason})
	return nil
}

// RecordReceipt books an arrival against one line and re-derives the status.
//
// This is the ONLY way received quantity changes, and the status that follows is
// computed here from the lines rather than supplied — so PARTIALLY_RECEIVED and
// COMPLETED can never disagree with the quantities they describe.
//
// It is called by the Goods Receipt flow, inside that flow's transaction. The
// aggregate does not verify that stock physically arrived; it verifies that this
// order is one that may be received against at all, which is the rule that keeps
// receipts off draft and cancelled orders.
func (o *PurchaseOrder) RecordReceipt(
	lineID uuid.UUID, amount Quantity, actor uuid.UUID, now time.Time,
) error {
	const op = "purchaseorder.entity.RecordReceipt"

	if !o.status.IsOpenForReceipt() {
		return apperror.Conflict(
			"goods cannot be received against a " + o.status.String() + " purchase order").
			WithOp(op).
			WithDetails(map[string]any{"status": o.status.String()})
	}

	index := -1
	for i, line := range o.lines {
		if line.ID() == lineID {
			index = i
			break
		}
	}
	if index < 0 {
		return apperror.NotFound("purchase order line not found on this order").
			WithOp(op).
			WithDetails(map[string]any{"line_id": lineID})
	}

	updated, err := o.lines[index].receive(amount)
	if err != nil {
		return err
	}
	o.lines[index] = updated

	previous := o.status
	o.status = o.deriveStatus()
	o.touch(actor, now)
	o.record(EventOrderReceiptRecorded, actor, now, map[string]any{
		"line_id":         lineID.String(),
		"quantity":        amount.Value(),
		"status":          o.status.String(),
		"previous_status": previous.String(),
	})
	if o.status == StatusCompleted && previous != StatusCompleted {
		o.record(EventOrderCompleted, actor, now, map[string]any{"line_count": len(o.lines)})
	}
	return nil
}

// deriveStatus computes the status implied by the line quantities.
func (o *PurchaseOrder) deriveStatus() Status {
	if o.IsFullyReceived() {
		return StatusCompleted
	}
	if o.HasReceipts() {
		return StatusPartiallyReceived
	}
	// Nothing received yet: an order that was open stays open.
	return StatusApproved
}

// ---------------------------------------------------------------------------
// Downstream gates
// ---------------------------------------------------------------------------

// CanGenerateASN reports whether an Advance Shipping Notice may be raised.
//
// True for an APPROVED or PARTIALLY_RECEIVED order that still has something
// outstanding. False for DRAFT (not committed), CANCELLED (withdrawn) and
// COMPLETED (nothing left to ship).
//
// The ASN module does not exist yet. This predicate is the seam it will consult,
// declared here so the rule lives with the aggregate that owns it rather than
// being re-derived by whichever module happens to need it first.
func (o *PurchaseOrder) CanGenerateASN() bool {
	return o.status.IsOpenForReceipt() && !o.IsFullyReceived()
}

// CanBeDeleted reports whether the order may be removed outright.
//
// Only a DRAFT. An approved order has been communicated to a supplier and a
// received one has stock behind it; both are cancelled, not deleted, so the
// record of what was committed survives.
func (o *PurchaseOrder) CanBeDeleted() bool { return o.status == StatusDraft }

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

func (o *PurchaseOrder) requireEditable(op string) error {
	if o.status.IsEditable() {
		return nil
	}
	return apperror.Conflict("a " + o.status.String() + " purchase order cannot be edited").
		WithOp(op).
		WithDetails(map[string]any{"status": o.status.String()})
}

func (o *PurchaseOrder) illegalTransition(target Status, op string) error {
	return apperror.Conflict(
		"a " + o.status.String() + " purchase order cannot become " + target.String()).
		WithOp(op).
		WithDetails(map[string]any{"status": o.status.String(), "target": target.String()})
}

func validateParties(supplierID, warehouseID uuid.UUID, op string) error {
	if supplierID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "supplier_id", Rule: "required",
			Message: "supplier is required",
		}).WithOp(op)
	}
	if warehouseID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "warehouse_id", Rule: "required",
			Message: "destination warehouse is required",
		}).WithOp(op)
	}
	return nil
}

// validateDates checks both dates are present and ordered.
//
// An expected arrival before the order date describes a delivery that precedes
// the order, which is a data-entry error rather than a plan.
func validateDates(orderDate, expectedArrivalDate time.Time, op string) error {
	if orderDate.IsZero() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "order_date", Rule: "required",
			Message: "order date is required",
		}).WithOp(op)
	}
	if expectedArrivalDate.IsZero() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "expected_arrival_date", Rule: "required",
			Message: "expected arrival date is required",
		}).WithOp(op)
	}
	if expectedArrivalDate.Before(orderDate) {
		return apperror.NewValidation(apperror.FieldError{
			Field: "expected_arrival_date", Rule: "after",
			Message: "expected arrival date cannot be before the order date",
		}).WithOp(op)
	}
	return nil
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

// assertNoDuplicateProducts refuses the same product twice on one order.
//
// Two lines for one article are almost always a double-entered row. Worse, they
// make receiving ambiguous: a receipt naming a product rather than a line would
// have no way to choose between them. A caller ordering more units raises the
// quantity on the single line.
func assertNoDuplicateProducts(lines []PurchaseOrderLine, op string) error {
	seen := make(map[uuid.UUID]struct{}, len(lines))
	for _, line := range lines {
		if _, duplicate := seen[line.ProductID()]; duplicate {
			return apperror.NewValidation(apperror.FieldError{
				Field: "lines", Rule: "duplicate",
				Message: "the same product appears on more than one line; combine them into one",
			}).WithOp(op)
		}
		seen[line.ProductID()] = struct{}{}
	}
	return nil
}

// touch records who last changed the aggregate and when. It deliberately does
// NOT bump the version — that belongs to the repository.
func (o *PurchaseOrder) touch(actor uuid.UUID, now time.Time) {
	o.updatedBy = actor
	o.updatedAt = now
}

func copyID(id *uuid.UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	copied := *id
	return &copied
}

func copyTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	copied := *at
	return &copied
}

// ---------------------------------------------------------------------------
// Readers
// ---------------------------------------------------------------------------

// ID identifies the order.
func (o *PurchaseOrder) ID() uuid.UUID { return o.id }

// CompanyID is the owning tenant.
func (o *PurchaseOrder) CompanyID() uuid.UUID { return o.companyID }

// Number is the operator-facing document number.
func (o *PurchaseOrder) Number() OrderNumber { return o.number }

// SupplierID is who the goods are bought from.
func (o *PurchaseOrder) SupplierID() uuid.UUID { return o.supplierID }

// WarehouseID is where the goods are expected.
func (o *PurchaseOrder) WarehouseID() uuid.UUID { return o.warehouseID }

// OrderDate is when the order was placed.
func (o *PurchaseOrder) OrderDate() time.Time { return o.orderDate }

// ExpectedArrivalDate is when the goods are due.
func (o *PurchaseOrder) ExpectedArrivalDate() time.Time { return o.expectedArrivalDate }

// Status is the current lifecycle state.
func (o *PurchaseOrder) Status() Status { return o.status }

// Remarks is the free-text note.
func (o *PurchaseOrder) Remarks() string { return o.remarks }

// Lines returns a COPY of the line set, so a caller cannot append to or rewrite
// the aggregate's slice behind its back.
func (o *PurchaseOrder) Lines() []PurchaseOrderLine {
	out := make([]PurchaseOrderLine, len(o.lines))
	copy(out, o.lines)
	return out
}

// LineCount reports how many lines the order carries.
func (o *PurchaseOrder) LineCount() int { return len(o.lines) }

// IsEditable reports whether the document may still be changed.
func (o *PurchaseOrder) IsEditable() bool { return o.status.IsEditable() }

// IsOpenForReceipt reports whether goods may be booked against this order.
func (o *PurchaseOrder) IsOpenForReceipt() bool { return o.status.IsOpenForReceipt() }

// HasReceipts reports whether any quantity has arrived.
func (o *PurchaseOrder) HasReceipts() bool {
	for _, line := range o.lines {
		if !line.ReceivedQty().IsZero() {
			return true
		}
	}
	return false
}

// IsFullyReceived reports whether every line is satisfied. An order with no lines
// is NOT fully received — there is nothing to satisfy, and reporting true would
// let an empty order derive itself into COMPLETED.
func (o *PurchaseOrder) IsFullyReceived() bool {
	if len(o.lines) == 0 {
		return false
	}
	for _, line := range o.lines {
		if !line.IsFullyReceived() {
			return false
		}
	}
	return true
}

func (o *PurchaseOrder) receivedLineCount() int {
	count := 0
	for _, line := range o.lines {
		if !line.ReceivedQty().IsZero() {
			count++
		}
	}
	return count
}

// TotalOrderedQty sums the ordered quantity across every line.
func (o *PurchaseOrder) TotalOrderedQty() int64 {
	var total int64
	for _, line := range o.lines {
		total += line.OrderedQty().Value()
	}
	return total
}

// TotalReceivedQty sums the received quantity across every line.
func (o *PurchaseOrder) TotalReceivedQty() int64 {
	var total int64
	for _, line := range o.lines {
		total += line.ReceivedQty().Value()
	}
	return total
}

// Version is the optimistic-lock token.
func (o *PurchaseOrder) Version() uint64 { return o.version }

// BelongsTo reports whether the order is owned by the given tenant. It backs the
// defence-in-depth check in the service layer.
func (o *PurchaseOrder) BelongsTo(companyID uuid.UUID) bool { return o.companyID == companyID }

// CreatedBy is the actor who drafted the order.
func (o *PurchaseOrder) CreatedBy() uuid.UUID { return o.createdBy }

// ApprovedBy is the actor who approved it, or nil while unapproved. The returned
// pointer is a COPY, so a caller cannot rewrite the aggregate through it.
func (o *PurchaseOrder) ApprovedBy() *uuid.UUID { return copyID(o.approvedBy) }

// ApprovedAt is when it was approved, or nil while unapproved.
func (o *PurchaseOrder) ApprovedAt() *time.Time { return copyTime(o.approvedAt) }

// UpdatedBy is the actor who last changed it.
func (o *PurchaseOrder) UpdatedBy() uuid.UUID { return o.updatedBy }

// CreatedAt is when it was drafted.
func (o *PurchaseOrder) CreatedAt() time.Time { return o.createdAt }

// UpdatedAt is when it last changed.
func (o *PurchaseOrder) UpdatedAt() time.Time { return o.updatedAt }
