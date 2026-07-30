package entity

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// PurchaseOrderLine is one article on a purchase order.
//
// It is a CHILD ENTITY, not an aggregate root: it has identity, but it is only
// ever reached through its PurchaseOrder. That is what makes "an approved order's
// lines cannot change" enforceable — there is no handle on a line that bypasses
// the root, so no code path can edit one without the root's permission.
//
// # Why receiving lives here
//
// The received quantity is the ONE mutable field, and it moves through
// receive(), which is unexported: only the aggregate may call it. A caller books
// a receipt against the ORDER, and the order decides which line it lands on and
// what the resulting order status is. Letting a caller write a line's received
// quantity directly would make the order's status derivable from data the order
// did not see change.
type PurchaseOrderLine struct {
	id uuid.UUID

	productID uuid.UUID
	uomID     uuid.UUID

	orderedQty  Quantity
	receivedQty Quantity

	unitPrice Money
	remarks   string
}

const maxLineRemarks = 500

// NewPurchaseOrderLine builds a line with nothing received yet.
func NewPurchaseOrderLine(
	id, productID, uomID uuid.UUID,
	orderedQty Quantity,
	unitPrice Money,
	remarks string,
) (PurchaseOrderLine, error) {
	return ReconstituteLine(id, productID, uomID, orderedQty, MustQuantity(0), unitPrice, remarks)
}

// ReconstituteLine rebuilds a line from a persisted row, received quantity and
// all.
//
// It is the seam the repository needs: the fields are unexported, so without an
// explicit constructor a stored line could never be loaded back. It performs the
// same validation as the factory — a row that violates an invariant is corrupt
// and must fail loudly at the boundary rather than travel into the domain.
func ReconstituteLine(
	id, productID, uomID uuid.UUID,
	orderedQty, receivedQty Quantity,
	unitPrice Money,
	remarks string,
) (PurchaseOrderLine, error) {
	const op = "purchaseorder.entity.ReconstituteLine"

	if id == uuid.Nil {
		return PurchaseOrderLine{}, apperror.Validation("line id is required").WithOp(op)
	}
	if productID == uuid.Nil {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "product_id", Rule: "required",
			Message: "product is required",
		}).WithOp(op)
	}
	if uomID == uuid.Nil {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "uom_id", Rule: "required",
			Message: "unit of measure is required",
		}).WithOp(op)
	}
	// Ordered quantity is strictly positive here, though Quantity itself allows
	// zero: a line that orders nothing is not an order for anything.
	if orderedQty.IsZero() {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "ordered_qty", Rule: "gt",
			Message: "ordered quantity must be greater than zero",
		}).WithOp(op)
	}
	if receivedQty.Value() > orderedQty.Value() {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "received_qty", Rule: "lte",
			Message: "received quantity cannot exceed the ordered quantity",
		}).WithOp(op)
	}
	if len(remarks) > maxLineRemarks {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "remarks", Rule: "max",
			Message: "line remarks must be at most 500 characters",
		}).WithOp(op)
	}

	return PurchaseOrderLine{
		id:          id,
		productID:   productID,
		uomID:       uomID,
		orderedQty:  orderedQty,
		receivedQty: receivedQty,
		unitPrice:   unitPrice,
		remarks:     remarks,
	}, nil
}

// receive books an arrival against this line and returns the updated line.
//
// Unexported and value-returning: only the aggregate may call it, and it cannot
// mutate a line the aggregate did not deliberately replace.
//
// Over-receipt is REFUSED rather than clamped. Clamping would silently discard
// the difference, leaving the order claiming it received exactly what it ordered
// while more stock physically arrived than the document accounts for. A genuine
// over-delivery is a commercial decision — amend the order, or book the excess
// against no order at all.
func (l PurchaseOrderLine) receive(amount Quantity) (PurchaseOrderLine, error) {
	const op = "purchaseorder.entity.PurchaseOrderLine.receive"

	if amount.IsZero() {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "quantity", Rule: "gt",
			Message: "received quantity must be greater than zero",
		}).WithOp(op)
	}

	total, err := l.receivedQty.Add(amount)
	if err != nil {
		return PurchaseOrderLine{}, err
	}
	if total.Value() > l.orderedQty.Value() {
		return PurchaseOrderLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "quantity", Rule: "lte",
			Message: "receiving this quantity would exceed the ordered quantity",
		}).WithOp(op)
	}

	l.receivedQty = total
	return l, nil
}

// ID identifies the line.
func (l PurchaseOrderLine) ID() uuid.UUID { return l.id }

// ProductID is the article ordered.
func (l PurchaseOrderLine) ProductID() uuid.UUID { return l.productID }

// UOMID is the unit the quantities are expressed in.
func (l PurchaseOrderLine) UOMID() uuid.UUID { return l.uomID }

// OrderedQty is how many units were ordered.
func (l PurchaseOrderLine) OrderedQty() Quantity { return l.orderedQty }

// ReceivedQty is how many units have arrived so far.
func (l PurchaseOrderLine) ReceivedQty() Quantity { return l.receivedQty }

// RemainingQty is DERIVED, never stored: ordered minus received.
//
// Deriving it removes a whole class of bug. A stored remaining column is a second
// source of truth that can drift from the two numbers it summarises, and every
// write would have to remember to keep all three consistent.
func (l PurchaseOrderLine) RemainingQty() Quantity {
	return Quantity{value: l.orderedQty.Value() - l.receivedQty.Value()}
}

// IsFullyReceived reports whether nothing is outstanding on this line.
func (l PurchaseOrderLine) IsFullyReceived() bool { return l.RemainingQty().IsZero() }

// UnitPrice is the optional commercial value of one unit.
func (l PurchaseOrderLine) UnitPrice() Money { return l.unitPrice }

// Remarks is the free-text note.
func (l PurchaseOrderLine) Remarks() string { return l.remarks }
