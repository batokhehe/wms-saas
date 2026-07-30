package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

const maxLineRemarks = 500

// GoodsReceiptLine is one article arriving on a receipt.
//
// It is a CHILD ENTITY, not an aggregate root: it has identity but no life of its
// own, and is only ever reached through its GoodsReceipt. That is what makes
// "a confirmed receipt's lines cannot change" enforceable — there is no handle on
// a line that bypasses the root.
//
// Its fields are unexported and it exposes no setters: a line is replaced, never
// edited in place.
type GoodsReceiptLine struct {
	id uuid.UUID

	productID  uuid.UUID
	locationID uuid.UUID
	uomID      uuid.UUID

	quantity Quantity

	batchNumber   string
	lotNumber     string
	serialNumbers []string

	expiryDate *time.Time
	remarks    string
}

// NewGoodsReceiptLine builds a validated line.
//
// Lot and serials are MUTUALLY EXCLUSIVE: they are two different ways of
// individuating the same units, and a line claiming both describes stock that is
// simultaneously tracked in bulk and item by item. When serials are given there
// must be exactly one per unit, because a serial identifies one physical item and
// cannot stand for two.
func NewGoodsReceiptLine(
	id, productID, locationID, uomID uuid.UUID,
	quantity Quantity,
	batchNumber, lotNumber string,
	serialNumbers []string,
	expiryDate *time.Time,
	remarks string,
) (GoodsReceiptLine, error) {
	const op = "goodsreceipt.entity.NewGoodsReceiptLine"

	if id == uuid.Nil {
		return GoodsReceiptLine{}, apperror.Validation("line id is required").WithOp(op)
	}
	if productID == uuid.Nil {
		return GoodsReceiptLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "product_id", Rule: "required",
			Message: "product is required",
		}).WithOp(op)
	}
	if locationID == uuid.Nil {
		return GoodsReceiptLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "location_id", Rule: "required",
			Message: "receiving location is required",
		}).WithOp(op)
	}
	if uomID == uuid.Nil {
		return GoodsReceiptLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "uom_id", Rule: "required",
			Message: "unit of measure is required",
		}).WithOp(op)
	}

	batchNumber = strings.TrimSpace(batchNumber)
	lotNumber = strings.TrimSpace(lotNumber)
	if len(batchNumber) > 64 {
		return GoodsReceiptLine{}, tooLong("batch_number", op)
	}
	if len(lotNumber) > 64 {
		return GoodsReceiptLine{}, tooLong("lot_number", op)
	}
	if len(remarks) > maxLineRemarks {
		return GoodsReceiptLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "remarks", Rule: "max",
			Message: "line remarks must be at most 500 characters",
		}).WithOp(op)
	}

	cleaned, err := cleanSerials(serialNumbers, quantity, op)
	if err != nil {
		return GoodsReceiptLine{}, err
	}
	if lotNumber != "" && len(cleaned) > 0 {
		return GoodsReceiptLine{}, apperror.NewValidation(apperror.FieldError{
			Field: "serial_numbers", Rule: "conflict",
			Message: "a line names either a lot number or serial numbers, never both",
		}).WithOp(op)
	}

	return GoodsReceiptLine{
		id:            id,
		productID:     productID,
		locationID:    locationID,
		uomID:         uomID,
		quantity:      quantity,
		batchNumber:   batchNumber,
		lotNumber:     lotNumber,
		serialNumbers: cleaned,
		expiryDate:    copyTime(expiryDate),
		remarks:       remarks,
	}, nil
}

// ReconstituteLine rebuilds a line from a persisted row.
//
// It is the seam the repository needs: the fields are unexported, so without an
// explicit constructor a stored line could never be loaded back. It performs the
// same validation as the factory — a row that violates an invariant is corrupt
// and must fail loudly at the boundary rather than travel into the domain.
func ReconstituteLine(
	id, productID, locationID, uomID uuid.UUID,
	quantity Quantity,
	batchNumber, lotNumber string,
	serialNumbers []string,
	expiryDate *time.Time,
	remarks string,
) (GoodsReceiptLine, error) {
	return NewGoodsReceiptLine(id, productID, locationID, uomID, quantity,
		batchNumber, lotNumber, serialNumbers, expiryDate, remarks)
}

// cleanSerials trims, de-duplicates and count-checks a serial list.
func cleanSerials(serials []string, quantity Quantity, op string) ([]string, error) {
	cleaned := make([]string, 0, len(serials))
	seen := make(map[string]struct{}, len(serials))
	for _, serial := range serials {
		serial = strings.TrimSpace(serial)
		if serial == "" {
			continue
		}
		if len(serial) > 128 {
			return nil, tooLong("serial_numbers", op)
		}
		if _, duplicate := seen[serial]; duplicate {
			return nil, apperror.NewValidation(apperror.FieldError{
				Field: "serial_numbers", Rule: "duplicate",
				Message: "serial number " + serial + " is listed more than once",
			}).WithOp(op)
		}
		seen[serial] = struct{}{}
		cleaned = append(cleaned, serial)
	}
	if len(cleaned) > 0 && int64(len(cleaned)) != quantity.Value() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "serial_numbers", Rule: "count",
			Message: "a serial-tracked line must list exactly one serial number per unit",
		}).WithOp(op)
	}
	return cleaned, nil
}

func tooLong(field, op string) error {
	return apperror.NewValidation(apperror.FieldError{
		Field: field, Rule: "max",
		Message: field + " is too long",
	}).WithOp(op)
}

func copyTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	copied := *at
	return &copied
}

// ID identifies the line.
func (l GoodsReceiptLine) ID() uuid.UUID { return l.id }

// ProductID is the article received.
func (l GoodsReceiptLine) ProductID() uuid.UUID { return l.productID }

// LocationID is the bin the goods land in. Putaway moves them onward.
func (l GoodsReceiptLine) LocationID() uuid.UUID { return l.locationID }

// UOMID is the unit the quantity is expressed in.
func (l GoodsReceiptLine) UOMID() uuid.UUID { return l.uomID }

// Quantity is how many units arrived.
func (l GoodsReceiptLine) Quantity() Quantity { return l.quantity }

// BatchNumber returns the batch, or "" when unset.
func (l GoodsReceiptLine) BatchNumber() string { return l.batchNumber }

// LotNumber returns the lot, or "" when unset.
func (l GoodsReceiptLine) LotNumber() string { return l.lotNumber }

// SerialNumbers returns a COPY of the serials, so a caller holding the returned
// slice cannot reach into the line and rewrite it.
func (l GoodsReceiptLine) SerialNumbers() []string {
	if len(l.serialNumbers) == 0 {
		return nil
	}
	out := make([]string, len(l.serialNumbers))
	copy(out, l.serialNumbers)
	return out
}

// IsSerialTracked reports whether the line names individual serials.
func (l GoodsReceiptLine) IsSerialTracked() bool { return len(l.serialNumbers) > 0 }

// IsLotTracked reports whether the line names a lot.
func (l GoodsReceiptLine) IsLotTracked() bool { return l.lotNumber != "" }

// ExpiryDate returns a COPY of the expiry, or nil when unset.
func (l GoodsReceiptLine) ExpiryDate() *time.Time { return copyTime(l.expiryDate) }

// Remarks is the free-text note.
func (l GoodsReceiptLine) Remarks() string { return l.remarks }
