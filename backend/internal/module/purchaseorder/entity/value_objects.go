// Package entity holds the PurchaseOrder module's domain types.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror, and
// nothing from any web framework or ORM.
package entity

import (
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// OrderNumber
// ---------------------------------------------------------------------------

// OrderNumber is the operator-facing identifier of a purchase order.
//
// A value object rather than a plain string so "a purchase order always has a
// non-blank, canonicalised number" is enforced once, at construction, instead of
// being re-checked at every call site that accepts one. It is printed on the
// document the supplier receives, which is why the aggregate offers no way to
// change it after creation.
type OrderNumber struct {
	value string
}

// NewOrderNumber validates and canonicalises a purchase order number.
func NewOrderNumber(raw string) (OrderNumber, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case value == "":
		return OrderNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "required",
			Message: "purchase order number is required",
		}).WithOp("purchaseorder.entity.NewOrderNumber")
	case len(value) < 3 || len(value) > 32:
		return OrderNumber{}, apperror.NewValidation(apperror.FieldError{
			Field: "number", Rule: "length",
			Message: "purchase order number must be between 3 and 32 characters",
		}).WithOp("purchaseorder.entity.NewOrderNumber")
	}
	return OrderNumber{value: value}, nil
}

// String renders the number.
func (n OrderNumber) String() string { return n.value }

// IsZero reports whether the number is unset.
func (n OrderNumber) IsZero() bool { return n.value == "" }

// ---------------------------------------------------------------------------
// Quantity
// ---------------------------------------------------------------------------

// Quantity is a non-negative number of units.
//
// Non-negative rather than strictly positive because a received quantity of zero
// is the normal state of a line that has been ordered but not yet delivered. The
// stricter "an ORDERED quantity must be positive" rule belongs to the line, not
// to the type, and is enforced there.
type Quantity struct {
	value int64
}

// NewQuantity validates a quantity.
func NewQuantity(value int64) (Quantity, error) {
	if value < 0 {
		return Quantity{}, apperror.NewValidation(apperror.FieldError{
			Field: "quantity", Rule: "gte",
			Message: "quantity cannot be negative",
		}).WithOp("purchaseorder.entity.NewQuantity")
	}
	return Quantity{value: value}, nil
}

// MustQuantity builds a quantity from a value already validated by the caller.
// It panics on a negative input, which would be a programming error.
func MustQuantity(value int64) Quantity {
	quantity, err := NewQuantity(value)
	if err != nil {
		panic(err)
	}
	return quantity
}

// Value returns the underlying count.
func (q Quantity) Value() int64 { return q.value }

// IsZero reports whether the quantity is nil.
func (q Quantity) IsZero() bool { return q.value == 0 }

// Add returns the sum, guarding against overflow.
func (q Quantity) Add(other Quantity) (Quantity, error) {
	sum := q.value + other.value
	if sum < q.value {
		return Quantity{}, apperror.Validation("quantity overflow").
			WithOp("purchaseorder.entity.Quantity.Add")
	}
	return Quantity{value: sum}, nil
}

// ---------------------------------------------------------------------------
// Money
// ---------------------------------------------------------------------------

// Money is an optional unit price, held in MINOR UNITS as a whole number.
//
// Integer minor units rather than a float: a float cannot represent most decimal
// prices exactly, and a rounding error on a purchase order becomes a rounding
// error on an invoice. Currency is NOT modelled — the system is single-currency
// today, and inventing a currency field that nothing sets or reads would be a
// promise the software does not keep.
type Money struct {
	amount int64
	set    bool
}

// NoMoney returns the unset price, for lines that carry no commercial value.
func NoMoney() Money { return Money{} }

// NewMoney validates a unit price in minor units.
func NewMoney(minorUnits int64) (Money, error) {
	if minorUnits < 0 {
		return Money{}, apperror.NewValidation(apperror.FieldError{
			Field: "unit_price", Rule: "gte",
			Message: "unit price cannot be negative",
		}).WithOp("purchaseorder.entity.NewMoney")
	}
	return Money{amount: minorUnits, set: true}, nil
}

// Amount returns the price in minor units. Zero when unset — callers that need
// to tell "free" from "not priced" use IsZero.
func (m Money) Amount() int64 { return m.amount }

// IsZero reports whether no price was supplied.
func (m Money) IsZero() bool { return !m.set }

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// Status is a purchase order's position in its lifecycle.
type Status string

// The lifecycle states.
//
// DRAFT and APPROVED are author-driven; PARTIALLY_RECEIVED and COMPLETED are
// DERIVED — they are set by the aggregate as receipts are recorded, never chosen
// by a caller. That distinction is why there is no "mark as completed" endpoint:
// completion is a fact about quantities, not a decision.
const (
	// StatusDraft is editable in every respect and generates nothing.
	StatusDraft Status = "DRAFT"

	// StatusApproved is locked and may generate an ASN.
	StatusApproved Status = "APPROVED"

	// StatusPartiallyReceived means some but not all ordered quantity has arrived.
	StatusPartiallyReceived Status = "PARTIALLY_RECEIVED"

	// StatusCompleted means every ordered unit has been received. Terminal.
	StatusCompleted Status = "COMPLETED"

	// StatusCancelled means the order was called off. Terminal.
	StatusCancelled Status = "CANCELLED"
)

// NewStatus validates a status read from outside the aggregate.
func NewStatus(raw string) (Status, error) {
	status := Status(strings.ToUpper(strings.TrimSpace(raw)))
	if !status.Valid() {
		return "", apperror.NewValidation(apperror.FieldError{
			Field: "status", Rule: "oneof",
			Message: "status must be DRAFT, APPROVED, PARTIALLY_RECEIVED, COMPLETED or CANCELLED",
		}).WithOp("purchaseorder.entity.NewStatus")
	}
	return status, nil
}

// Valid reports whether the status is a known value.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusApproved, StatusPartiallyReceived,
		StatusCompleted, StatusCancelled:
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
// Only DRAFT. Once approved, the order is a commitment the supplier has been
// told about; editing it afterwards would mean the paperwork and the commitment
// disagree. Every mutator consults this rather than comparing against
// StatusDraft directly, so a future PENDING_APPROVAL state changes one function.
func (s Status) IsEditable() bool { return s == StatusDraft }

// IsOpenForReceipt reports whether goods may be booked in against this order.
//
// This is the rule "a Goods Receipt cannot reference a DRAFT purchase order",
// expressed once so every caller asks the same question. A draft has not been
// committed to the supplier and a cancelled order has been withdrawn; in neither
// case should stock arriving be attributable to it.
func (s Status) IsOpenForReceipt() bool {
	return s == StatusApproved || s == StatusPartiallyReceived
}
