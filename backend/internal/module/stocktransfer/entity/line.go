package entity

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// StockTransferLine is one product movement within a transfer.
//
// It is a CHILD ENTITY, not an aggregate root: it has identity (two lines for
// the same product from the same bin are distinct records) but it has no life of
// its own. It is only ever reached through its StockTransfer, which is what
// makes "a confirmed transfer's lines cannot change" enforceable — there is no
// handle on a line that bypasses the root.
//
// Its fields are unexported and it exposes no setters, for the same reason the
// root does: a line is replaced, never edited in place.
type StockTransferLine struct {
	id uuid.UUID

	productID      uuid.UUID
	fromLocationID uuid.UUID
	toLocationID   uuid.UUID

	quantity   Quantity
	attributes LineAttributes
}

// NewStockTransferLine builds a validated line.
//
// The source and destination locations must differ. A line that moves stock to
// where it already is changes nothing, but would still be executed against the
// inventory service and would still append a ledger entry — recording a movement
// that did not happen.
func NewStockTransferLine(
	id, productID, fromLocationID, toLocationID uuid.UUID,
	quantity Quantity,
	attributes LineAttributes,
) (StockTransferLine, error) {
	const op = "stocktransfer.entity.NewStockTransferLine"

	if id == uuid.Nil {
		return StockTransferLine{}, apperror.Validation("line id is required").WithOp(op)
	}
	if productID == uuid.Nil {
		return StockTransferLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "product_id", Rule: "required",
			Message: "product is required",
		}).WithOp(op)
	}
	if fromLocationID == uuid.Nil {
		return StockTransferLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "from_location_id", Rule: "required",
			Message: "source location is required",
		}).WithOp(op)
	}
	if toLocationID == uuid.Nil {
		return StockTransferLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "to_location_id", Rule: "required",
			Message: "destination location is required",
		}).WithOp(op)
	}
	if fromLocationID == toLocationID {
		return StockTransferLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "to_location_id", Rule: "mismatch",
			Message: "destination location must differ from the source location",
		}).WithOp(op)
	}

	return StockTransferLine{
		id:             id,
		productID:      productID,
		fromLocationID: fromLocationID,
		toLocationID:   toLocationID,
		quantity:       quantity,
		attributes:     attributes,
	}, nil
}

// ReconstituteLine rebuilds a line from a persisted row.
//
// It is the seam the repository needs: the fields are unexported, so without an
// explicit constructor a stored line could never be loaded back. It performs the
// same validation as the factory — a row that violates an invariant is corrupt
// and must fail loudly at the boundary rather than travel into the domain.
func ReconstituteLine(
	id, productID, fromLocationID, toLocationID uuid.UUID,
	quantity Quantity,
	attributes LineAttributes,
) (StockTransferLine, error) {
	return NewStockTransferLine(id, productID, fromLocationID, toLocationID, quantity, attributes)
}

// ID identifies the line.
func (l StockTransferLine) ID() uuid.UUID { return l.id }

// ProductID is the article being moved.
func (l StockTransferLine) ProductID() uuid.UUID { return l.productID }

// FromLocationID is the source bin.
func (l StockTransferLine) FromLocationID() uuid.UUID { return l.fromLocationID }

// ToLocationID is the destination bin.
func (l StockTransferLine) ToLocationID() uuid.UUID { return l.toLocationID }

// Quantity is how many units move.
func (l StockTransferLine) Quantity() Quantity { return l.quantity }

// Attributes individuate the stock the line moves.
func (l StockTransferLine) Attributes() LineAttributes { return l.attributes }
