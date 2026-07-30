// Package entity holds the GoodsReceipt module's domain types.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror, and
// nothing from any web framework or ORM.
package entity

import (
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// ReceiptNumber
// ---------------------------------------------------------------------------

// ReceiptNumber is the operator-facing identifier of a goods receipt.
type ReceiptNumber struct {
	value string
}

// NewReceiptNumber validates and canonicalises a receipt number.
func NewReceiptNumber(raw string) (ReceiptNumber, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case value == "":
		return ReceiptNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "required",
			Message: "goods receipt number is required",
		}).WithOp("goodsreceipt.entity.NewReceiptNumber")
	case len(value) < 3 || len(value) > 32:
		return ReceiptNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "length",
			Message: "goods receipt number must be between 3 and 32 characters",
		}).WithOp("goodsreceipt.entity.NewReceiptNumber")
	}
	return ReceiptNumber{value: value}, nil
}

// String renders the number.
func (n ReceiptNumber) String() string { return n.value }

// IsZero reports whether the number is unset.
func (n ReceiptNumber) IsZero() bool { return n.value == "" }

// ---------------------------------------------------------------------------
// Quantity
// ---------------------------------------------------------------------------

// Quantity is a strictly positive number of whole units.
//
// Whole units, not a decimal: stock is counted in whole units everywhere in this
// system, and a fractional receipt could never be posted to an inventory position
// that cannot hold one — the receipt and the stock it creates would not reconcile.
//
// Strictly positive: a zero-unit line receives nothing but would still post a
// movement and append a ledger entry, recording an arrival that did not happen.
type Quantity struct {
	value int64
}

// NewQuantity validates a received quantity.
func NewQuantity(value int64) (Quantity, error) {
	if value <= 0 {
		return Quantity{}, apperror.NewValidation(apperror.FieldError{
			Field: "quantity", Rule: "gt",
			Message: "quantity must be greater than zero",
		}).WithOp("goodsreceipt.entity.NewQuantity")
	}
	return Quantity{value: value}, nil
}

// Value returns the underlying count.
func (q Quantity) Value() int64 { return q.value }

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status is a receipt's position in its lifecycle.
type Status string

// The lifecycle states.
//
// DRAFT and CONFIRMED are working states; RECEIVED and CANCELLED are TERMINAL.
// RECEIVED is terminal because it is the point at which stock was actually
// booked into inventory — editing the paperwork afterwards would leave the
// document contradicting the ledger that witnessed the movement.
const (
	// StatusDraft is editable in every respect.
	StatusDraft Status = "DRAFT"

	// StatusConfirmed is locked: the receipt is checked and awaiting posting.
	StatusConfirmed Status = "CONFIRMED"

	// StatusReceived means the stock has been posted to inventory. Terminal.
	StatusReceived Status = "RECEIVED"

	// StatusCancelled means the receipt was abandoned before posting. Terminal.
	StatusCancelled Status = "CANCELLED"
)

// NewStatus validates a status read from outside the aggregate.
func NewStatus(raw string) (Status, error) {
	status := Status(strings.ToUpper(strings.TrimSpace(raw)))
	if !status.Valid() {
		return "", apperror.NewValidation(apperror.FieldError{
			Field: "status", Rule: "oneof",
			Message: "status must be DRAFT, CONFIRMED, RECEIVED or CANCELLED",
		}).WithOp("goodsreceipt.entity.NewStatus")
	}
	return status, nil
}

// Valid reports whether the status is a known value.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusConfirmed, StatusReceived, StatusCancelled:
		return true
	default:
		return false
	}
}

// String renders the status.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether no further transition is possible.
func (s Status) IsTerminal() bool {
	return s == StatusReceived || s == StatusCancelled
}

// IsEditable reports whether the document's contents may still be changed.
//
// Only DRAFT. Every mutator consults this rather than comparing against
// StatusDraft itself, so a future PENDING_INSPECTION state changes one function.
func (s Status) IsEditable() bool { return s == StatusDraft }

// ---------------------------------------------------------------------------
// DocumentReference
// ---------------------------------------------------------------------------

// ReferenceType names the kind of document a receipt was raised against.
type ReferenceType string

// The reference kinds.
//
// NONE is the "manual receipt" case: stock arriving against no planning document
// at all. It is deliberately a NAMED value rather than an empty string, so a
// receipt that references nothing says so rather than merely omitting a field.
const (
	// ReferenceNone is a manual receipt, raised against no planning document.
	ReferenceNone ReferenceType = "NONE"

	// ReferencePurchaseOrder points at a purchase order.
	ReferencePurchaseOrder ReferenceType = "PURCHASE_ORDER"

	// ReferenceASN points at an advance shipping notice. The ASN module does not
	// exist yet; the value is declared so the enum does not have to change when
	// it arrives, and nothing produces it today.
	ReferenceASN ReferenceType = "ASN"
)

// Valid reports whether the reference type is a known value.
func (r ReferenceType) Valid() bool {
	switch r {
	case ReferenceNone, ReferencePurchaseOrder, ReferenceASN:
		return true
	default:
		return false
	}
}

// String renders the reference type.
func (r ReferenceType) String() string { return string(r) }
