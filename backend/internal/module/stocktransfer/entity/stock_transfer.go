package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// StockTransfer is the aggregate root for moving stock between locations.
//
// # What it is and is not responsible for
//
// The transfer is a DOCUMENT. It records an intent to move stock, who approved
// it, and whether it has been executed. It does NOT hold stock and does not
// know any balance: the quantity a company owns is InventoryPosition's business
// and nothing here changes it.
//
// That division is the whole point of the module. A transfer moves stock from
// one place to another, so total quantity is invariant across it — and the only
// way to guarantee that is to never let this aggregate write a quantity. When a
// transfer completes, the application service asks the inventory service to
// perform the move; the inventory aggregate debits the origin and credits the
// destination in one transaction, and the ledger witnesses both halves.
//
// # Encapsulation
//
// Every field is unexported and there are no setters. The lifecycle rule "DRAFT
// is editable, CONFIRMED is locked" is therefore not a convention a caller must
// remember — it is unreachable state. There is no code path that edits a
// confirmed transfer, because there is no method that would.
type StockTransfer struct {
	id        uuid.UUID
	companyID uuid.UUID

	number TransferNumber

	fromWarehouseID uuid.UUID
	toWarehouseID   uuid.UUID

	status       Status
	transferDate time.Time
	remarks      string

	lines []StockTransferLine

	// version is the optimistic-lock token. It is READ-ONLY to the domain: no
	// behaviour below touches it. The repository is its sole owner, because the
	// version tracks a ROW's revision, which is a persistence fact.
	version uint64

	createdBy, updatedBy uuid.UUID
	createdAt, updatedAt time.Time

	events []Event
}

const maxRemarksLength = 1000

// NewStockTransfer opens a transfer in DRAFT.
//
// A transfer starts empty and editable. Lines are added afterwards rather than
// passed here, because a document being drafted legitimately has no lines yet,
// and forcing a caller to supply them at creation would make "save my work in
// progress" impossible.
func NewStockTransfer(
	id, companyID uuid.UUID,
	number TransferNumber,
	fromWarehouseID, toWarehouseID uuid.UUID,
	transferDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) (*StockTransfer, error) {
	const op = "stocktransfer.entity.NewStockTransfer"

	if id == uuid.Nil {
		return nil, apperror.Validation("transfer id is required").WithOp(op)
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
			Message: "transfer number is required",
		}).WithOp(op)
	}
	if err := validateRoute(fromWarehouseID, toWarehouseID, op); err != nil {
		return nil, err
	}
	if err := validateRemarks(remarks, op); err != nil {
		return nil, err
	}
	if transferDate.IsZero() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "transfer_date", Rule: "required",
			Message: "transfer date is required",
		}).WithOp(op)
	}

	transfer := &StockTransfer{
		id:              id,
		companyID:       companyID,
		number:          number,
		fromWarehouseID: fromWarehouseID,
		toWarehouseID:   toWarehouseID,
		status:          StatusDraft,
		transferDate:    transferDate,
		remarks:         remarks,
		version:         1,
		createdBy:       actor,
		updatedBy:       actor,
		createdAt:       now,
		updatedAt:       now,
	}
	transfer.record(EventTransferCreated, actor, now, map[string]any{
		"number":            number.String(),
		"from_warehouse_id": fromWarehouseID.String(),
		"to_warehouse_id":   toWarehouseID.String(),
		"same_warehouse":    fromWarehouseID == toWarehouseID,
	})
	return transfer, nil
}

// Reconstitute rebuilds a transfer from persisted state WITHOUT raising events.
//
// Loading a row is not a business event. If this raised the creation event, every
// read would re-announce a transfer that was created once, long ago.
func Reconstitute(
	id, companyID uuid.UUID,
	number TransferNumber,
	fromWarehouseID, toWarehouseID uuid.UUID,
	status Status,
	transferDate time.Time,
	remarks string,
	lines []StockTransferLine,
	version uint64,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*StockTransfer, error) {
	const op = "stocktransfer.entity.Reconstitute"

	if id == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("transfer identity is incomplete").WithOp(op)
	}
	if number.IsZero() {
		return nil, apperror.Validation("stored transfer has no number").WithOp(op)
	}
	if !status.Valid() {
		return nil, apperror.Validation("stored transfer has an invalid status").WithOp(op)
	}
	if version == 0 {
		return nil, apperror.Validation("stored transfer has version zero").WithOp(op)
	}
	if err := validateRoute(fromWarehouseID, toWarehouseID, op); err != nil {
		return nil, err
	}

	restored := make([]StockTransferLine, len(lines))
	copy(restored, lines)

	return &StockTransfer{
		id:              id,
		companyID:       companyID,
		number:          number,
		fromWarehouseID: fromWarehouseID,
		toWarehouseID:   toWarehouseID,
		status:          status,
		transferDate:    transferDate,
		remarks:         remarks,
		lines:           restored,
		version:         version,
		createdBy:       createdBy,
		updatedBy:       updatedBy,
		createdAt:       createdAt,
		updatedAt:       updatedAt,
	}, nil
}

// ---------------------------------------------------------------------------
// Behaviours — editing (DRAFT only)
// ---------------------------------------------------------------------------

// UpdateHeader replaces the editable header attributes.
//
// The warehouses are editable in DRAFT but the NUMBER is not: the number is how
// the document is referred to outside the system, and renaming it would strand
// every reference to it.
func (t *StockTransfer) UpdateHeader(
	fromWarehouseID, toWarehouseID uuid.UUID,
	transferDate time.Time,
	remarks string,
	actor uuid.UUID,
	now time.Time,
) error {
	const op = "stocktransfer.entity.UpdateHeader"

	if err := t.requireEditable(op); err != nil {
		return err
	}
	if err := validateRoute(fromWarehouseID, toWarehouseID, op); err != nil {
		return err
	}
	if err := validateRemarks(remarks, op); err != nil {
		return err
	}
	if transferDate.IsZero() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "transfer_date", Rule: "required",
			Message: "transfer date is required",
		}).WithOp(op)
	}

	t.fromWarehouseID = fromWarehouseID
	t.toWarehouseID = toWarehouseID
	t.transferDate = transferDate
	t.remarks = remarks
	t.touch(actor, now)
	t.record(EventTransferUpdated, actor, now, map[string]any{
		"from_warehouse_id": fromWarehouseID.String(),
		"to_warehouse_id":   toWarehouseID.String(),
	})
	return nil
}

// ReplaceLines swaps the whole line set.
//
// Replacement rather than add/remove/edit: the lines are a single value from the
// document's point of view, and a client editing a transfer sends the complete
// desired state. That removes a whole class of partial-update ambiguity ("does
// an absent line mean unchanged, or deleted?") without losing any capability.
func (t *StockTransfer) ReplaceLines(lines []StockTransferLine, actor uuid.UUID, now time.Time) error {
	const op = "stocktransfer.entity.ReplaceLines"

	if err := t.requireEditable(op); err != nil {
		return err
	}
	if err := assertNoDuplicateMovements(lines, op); err != nil {
		return err
	}

	replaced := make([]StockTransferLine, len(lines))
	copy(replaced, lines)
	t.lines = replaced

	t.touch(actor, now)
	t.record(EventTransferUpdated, actor, now, map[string]any{"line_count": len(replaced)})
	return nil
}

// ---------------------------------------------------------------------------
// Behaviours — lifecycle
// ---------------------------------------------------------------------------

// Confirm locks the document for execution.
//
// A transfer with no lines cannot be confirmed: confirming is an approval, and
// approving a document that moves nothing is meaningless — it would then be
// completable, producing a "completed" transfer that never touched stock.
func (t *StockTransfer) Confirm(actor uuid.UUID, now time.Time) error {
	const op = "stocktransfer.entity.Confirm"

	if t.status != StatusDraft {
		return t.illegalTransition(StatusConfirmed, op)
	}
	if len(t.lines) == 0 {
		return apperror.NewValidation(apperror.FieldError{
			Field: "lines", Rule: "required",
			Message: "a transfer must have at least one line before it can be confirmed",
		}).WithOp(op)
	}

	t.status = StatusConfirmed
	t.touch(actor, now)
	t.record(EventTransferConfirmed, actor, now, map[string]any{"line_count": len(t.lines)})
	return nil
}

// Complete marks the transfer executed.
//
// This is called by the application service AFTER the inventory service has
// moved every line, inside the same transaction. The aggregate does not perform
// the movement and cannot verify it happened — but it can refuse to be completed
// from any state other than CONFIRMED, which is what stops a draft from being
// marked done without ever being approved.
func (t *StockTransfer) Complete(actor uuid.UUID, now time.Time) error {
	const op = "stocktransfer.entity.Complete"

	if t.status != StatusConfirmed {
		return t.illegalTransition(StatusCompleted, op)
	}

	t.status = StatusCompleted
	t.touch(actor, now)
	t.record(EventTransferCompleted, actor, now, map[string]any{"line_count": len(t.lines)})
	return nil
}

// Cancel abandons the transfer.
//
// Permitted from DRAFT and CONFIRMED — an approved transfer can still be called
// off before the stock is picked up. It is NOT permitted from COMPLETED: once
// the stock has physically moved, cancelling the paperwork would leave the
// document contradicting the ledger. Reversing a completed transfer is a new
// transfer in the opposite direction, not an edit of this one.
func (t *StockTransfer) Cancel(reason string, actor uuid.UUID, now time.Time) error {
	const op = "stocktransfer.entity.Cancel"

	if t.status.IsTerminal() {
		return t.illegalTransition(StatusCancelled, op)
	}
	if err := validateRemarks(reason, op); err != nil {
		return err
	}

	t.status = StatusCancelled
	if reason != "" {
		t.remarks = reason
	}
	t.touch(actor, now)
	t.record(EventTransferCancelled, actor, now, map[string]any{"reason": reason})
	return nil
}

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

// requireEditable refuses a content change outside DRAFT.
func (t *StockTransfer) requireEditable(op string) error {
	if t.status.IsEditable() {
		return nil
	}
	return apperror.Conflict("a " + t.status.String() + " transfer cannot be edited").
		WithOp(op).
		WithDetails(map[string]any{"status": t.status.String()})
}

// illegalTransition reports a refused lifecycle move.
func (t *StockTransfer) illegalTransition(target Status, op string) error {
	return apperror.Conflict(
		"a " + t.status.String() + " transfer cannot become " + target.String()).
		WithOp(op).
		WithDetails(map[string]any{"status": t.status.String(), "target": target.String()})
}

// validateRoute checks the warehouse pair.
//
// from == to is ALLOWED and is the same-warehouse case: moving a pallet from a
// receiving bay to a rack inside one building is the commonest transfer there
// is. What is validated here is only that both are named.
func validateRoute(fromWarehouseID, toWarehouseID uuid.UUID, op string) error {
	if fromWarehouseID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "from_warehouse_id", Rule: "required",
			Message: "source warehouse is required",
		}).WithOp(op)
	}
	if toWarehouseID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "to_warehouse_id", Rule: "required",
			Message: "destination warehouse is required",
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

// assertNoDuplicateMovements refuses two lines that move the same stock along
// the same route.
//
// Two such lines are almost always a double-entered row, and executing both would
// move twice what the operator intended. A caller that genuinely wants to move
// more units raises the quantity on one line.
func assertNoDuplicateMovements(lines []StockTransferLine, op string) error {
	type movementKey struct {
		product, from, to uuid.UUID
		batch, lot        string
	}
	seen := make(map[movementKey]struct{}, len(lines))
	for _, line := range lines {
		key := movementKey{
			product: line.ProductID(),
			from:    line.FromLocationID(),
			to:      line.ToLocationID(),
			batch:   line.Attributes().BatchNumber(),
			lot:     line.Attributes().LotNumber(),
		}
		if _, duplicate := seen[key]; duplicate {
			return apperror.NewValidation(apperror.FieldError{
				Field: "lines", Rule: "duplicate",
				Message: "two lines move the same product along the same route; combine them into one",
			}).WithOp(op)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// touch records who last changed the aggregate and when. It deliberately does
// NOT bump the version — that belongs to the repository.
func (t *StockTransfer) touch(actor uuid.UUID, now time.Time) {
	t.updatedBy = actor
	t.updatedAt = now
}

// ---------------------------------------------------------------------------
// Readers
// ---------------------------------------------------------------------------

// ID identifies the transfer.
func (t *StockTransfer) ID() uuid.UUID { return t.id }

// CompanyID is the owning tenant.
func (t *StockTransfer) CompanyID() uuid.UUID { return t.companyID }

// Number is the operator-facing document number.
func (t *StockTransfer) Number() TransferNumber { return t.number }

// FromWarehouseID is the source site.
func (t *StockTransfer) FromWarehouseID() uuid.UUID { return t.fromWarehouseID }

// ToWarehouseID is the destination site.
func (t *StockTransfer) ToWarehouseID() uuid.UUID { return t.toWarehouseID }

// Status is the current lifecycle state.
func (t *StockTransfer) Status() Status { return t.status }

// TransferDate is the business date of the movement.
func (t *StockTransfer) TransferDate() time.Time { return t.transferDate }

// Remarks is the free-text note.
func (t *StockTransfer) Remarks() string { return t.remarks }

// Lines returns a COPY of the line set, so a caller cannot append to or rewrite
// the aggregate's slice behind its back.
func (t *StockTransfer) Lines() []StockTransferLine {
	out := make([]StockTransferLine, len(t.lines))
	copy(out, t.lines)
	return out
}

// LineCount reports how many lines the transfer carries.
func (t *StockTransfer) LineCount() int { return len(t.lines) }

// IsSameWarehouse reports whether the movement stays inside one site.
func (t *StockTransfer) IsSameWarehouse() bool { return t.fromWarehouseID == t.toWarehouseID }

// IsEditable reports whether the document may still be changed.
func (t *StockTransfer) IsEditable() bool { return t.status.IsEditable() }

// Version is the optimistic-lock token.
func (t *StockTransfer) Version() uint64 { return t.version }

// BelongsTo reports whether the transfer is owned by the given tenant. It backs
// the defence-in-depth check in the service layer.
func (t *StockTransfer) BelongsTo(companyID uuid.UUID) bool { return t.companyID == companyID }

// CreatedBy is the actor who opened the document.
func (t *StockTransfer) CreatedBy() uuid.UUID { return t.createdBy }

// UpdatedBy is the actor who last changed it.
func (t *StockTransfer) UpdatedBy() uuid.UUID { return t.updatedBy }

// CreatedAt is when it was opened.
func (t *StockTransfer) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt is when it last changed.
func (t *StockTransfer) UpdatedAt() time.Time { return t.updatedAt }
