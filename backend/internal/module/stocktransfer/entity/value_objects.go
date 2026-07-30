// Package entity holds the StockTransfer module's domain types.
//
// LAYER RULE: entity imports nothing from this project except
// internal/shared/entity and pkg/apperror, and nothing from any web framework
// or ORM.
package entity

import (
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// TransferNumber
// ---------------------------------------------------------------------------

// TransferNumber is the operator-facing identifier of a transfer document.
//
// It is a value object rather than a plain string so that "a transfer always has
// a non-blank, canonicalised number" is enforced once, at construction, instead
// of being re-checked at every call site that accepts one.
type TransferNumber struct {
	value string
}

// NewTransferNumber validates and canonicalises a transfer number.
func NewTransferNumber(raw string) (TransferNumber, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case value == "":
		return TransferNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "required",
			Message: "transfer number is required",
		}).WithOp("stocktransfer.entity.NewTransferNumber")
	case len(value) < 3 || len(value) > 32:
		return TransferNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "length",
			Message: "transfer number must be between 3 and 32 characters",
		}).WithOp("stocktransfer.entity.NewTransferNumber")
	}
	return TransferNumber{value: value}, nil
}

// String renders the number.
func (n TransferNumber) String() string { return n.value }

// IsZero reports whether the number is unset.
func (n TransferNumber) IsZero() bool { return n.value == "" }

// ---------------------------------------------------------------------------
// Quantity
// ---------------------------------------------------------------------------

// Quantity is a strictly positive number of units.
//
// Strictly positive, not merely non-negative: a transfer line for zero units
// moves nothing and would produce a ledger entry with a zero delta, which is
// noise in an audit trail rather than a record of anything.
type Quantity struct {
	value int64
}

// NewQuantity validates a transfer quantity.
func NewQuantity(value int64) (Quantity, error) {
	if value <= 0 {
		return Quantity{}, apperror.NewValidation(apperror.FieldError{
			Field: "quantity", Rule: "gt",
			Message: "quantity must be greater than zero",
		}).WithOp("stocktransfer.entity.NewQuantity")
	}
	return Quantity{value: value}, nil
}

// Value returns the underlying count.
func (q Quantity) Value() int64 { return q.value }

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status is a transfer's position in its lifecycle.
type Status string

// The lifecycle states.
//
// DRAFT and CONFIRMED are working states; COMPLETED and CANCELLED are TERMINAL.
// Terminality is what makes the document trustworthy: once stock has actually
// moved, the paperwork that authorised it must not be editable, or the ledger
// and the document would tell different stories about the same event.
const (
	// StatusDraft is editable in every respect.
	StatusDraft Status = "DRAFT"

	// StatusConfirmed is locked: the document is approved and awaiting execution.
	StatusConfirmed Status = "CONFIRMED"

	// StatusCompleted means the stock has moved. Terminal.
	StatusCompleted Status = "COMPLETED"

	// StatusCancelled means the transfer was abandoned before execution. Terminal.
	StatusCancelled Status = "CANCELLED"
)

// NewStatus validates a status read from outside the aggregate.
func NewStatus(raw string) (Status, error) {
	status := Status(strings.ToUpper(strings.TrimSpace(raw)))
	if !status.Valid() {
		return "", apperror.NewValidation(apperror.FieldError{
			Field: "status", Rule: "oneof",
			Message: "status must be DRAFT, CONFIRMED, COMPLETED or CANCELLED",
		}).WithOp("stocktransfer.entity.NewStatus")
	}
	return status, nil
}

// Valid reports whether the status is a known value.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusConfirmed, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

// String renders the status.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether no further transition is possible.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted || s == StatusCancelled
}

// IsEditable reports whether the document's contents may still be changed.
//
// Only DRAFT is editable. This single predicate is what "Draft editable,
// Confirmed locked" means in code, and every mutator consults it rather than
// comparing against StatusDraft itself — so a future PENDING_APPROVAL state
// changes one function, not fifteen call sites.
func (s Status) IsEditable() bool { return s == StatusDraft }

// ---------------------------------------------------------------------------
// LineAttributes
// ---------------------------------------------------------------------------

// LineAttributes individuate the stock a line moves: its batch, its lot, and
// the specific serial numbers when the product is serial-tracked.
//
// All three are optional, but they are validated TOGETHER because the valid
// combinations are a property of the set, not of any one field: a serial-tracked
// line must name exactly as many serials as it moves units, since a serial
// identifies one physical item and cannot stand for two.
type LineAttributes struct {
	batchNumber   string
	lotNumber     string
	serialNumbers []string
}

// NoLineAttributes returns the empty attribute set, for untracked stock.
func NoLineAttributes() LineAttributes { return LineAttributes{} }

// NewLineAttributes validates a batch/lot/serial combination against the number
// of units the line moves.
func NewLineAttributes(batch, lot string, serials []string, quantity Quantity) (LineAttributes, error) {
	const op = "stocktransfer.entity.NewLineAttributes"

	batch = strings.TrimSpace(batch)
	lot = strings.TrimSpace(lot)

	if len(batch) > 64 {
		return LineAttributes{}, apperror.NewValidation(apperror.FieldError{
			Field: "batch_number", Rule: "max",
			Message: "batch number must be at most 64 characters",
		}).WithOp(op)
	}
	if len(lot) > 64 {
		return LineAttributes{}, apperror.NewValidation(apperror.FieldError{
			Field: "lot_number", Rule: "max",
			Message: "lot number must be at most 64 characters",
		}).WithOp(op)
	}

	cleaned := make([]string, 0, len(serials))
	seen := make(map[string]struct{}, len(serials))
	for _, serial := range serials {
		serial = strings.TrimSpace(serial)
		if serial == "" {
			continue
		}
		if len(serial) > 128 {
			return LineAttributes{}, apperror.NewValidation(apperror.FieldError{
				Field: "serial_numbers", Rule: "max",
				Message: "a serial number must be at most 128 characters",
			}).WithOp(op)
		}
		// A serial names one physical item. Listing it twice on one line would
		// claim to move the same item two times.
		if _, duplicate := seen[serial]; duplicate {
			return LineAttributes{}, apperror.NewValidation(apperror.FieldError{
				Field: "serial_numbers", Rule: "duplicate",
				Message: "serial number " + serial + " is listed more than once",
			}).WithOp(op)
		}
		seen[serial] = struct{}{}
		cleaned = append(cleaned, serial)
	}

	if len(cleaned) > 0 && int64(len(cleaned)) != quantity.Value() {
		return LineAttributes{}, apperror.NewValidation(apperror.FieldError{
			Field: "serial_numbers", Rule: "count",
			Message: "a serial-tracked line must list exactly one serial number per unit",
		}).WithOp(op)
	}

	return LineAttributes{batchNumber: batch, lotNumber: lot, serialNumbers: cleaned}, nil
}

// BatchNumber returns the batch, or "" when unset.
func (a LineAttributes) BatchNumber() string { return a.batchNumber }

// LotNumber returns the lot, or "" when unset.
func (a LineAttributes) LotNumber() string { return a.lotNumber }

// SerialNumbers returns a COPY of the serials, so a caller holding the returned
// slice cannot reach into the value object and rewrite it.
func (a LineAttributes) SerialNumbers() []string {
	if len(a.serialNumbers) == 0 {
		return nil
	}
	out := make([]string, len(a.serialNumbers))
	copy(out, a.serialNumbers)
	return out
}

// IsSerialTracked reports whether the line names individual serials.
func (a LineAttributes) IsSerialTracked() bool { return len(a.serialNumbers) > 0 }

// IsZero reports whether no attribute is set.
func (a LineAttributes) IsZero() bool {
	return a.batchNumber == "" && a.lotNumber == "" && len(a.serialNumbers) == 0
}
