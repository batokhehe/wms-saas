package entity

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// DocumentReference is the planning document a receipt was raised against.
//
// It is a value object rather than two loose fields because the two are only
// meaningful together: a reference id with no type is unresolvable (nothing says
// which document it points at), and a type other than NONE with no id names a
// kind of document without naming one. Validating them as a pair is the only way
// to make both contradictions unrepresentable.
//
// This is the seam the inbound chain hangs from:
//
//	PurchaseOrder -> ASN -> GoodsReceipt
//
// A receipt referencing a PURCHASE_ORDER is what lets the order learn that its
// goods arrived. "Never create a Goods Receipt directly from a Supplier when a
// Purchase Order exists" is a policy the caller applies by choosing the reference
// — the aggregate's job is to make sure that whatever is chosen is coherent.
type DocumentReference struct {
	kind ReferenceType
	id   *uuid.UUID
}

// NoReference returns the manual-receipt reference: stock arriving against no
// planning document at all.
func NoReference() DocumentReference {
	return DocumentReference{kind: ReferenceNone}
}

// NewDocumentReference validates a type/id pair.
func NewDocumentReference(kind ReferenceType, id *uuid.UUID) (DocumentReference, error) {
	const op = "goodsreceipt.entity.NewDocumentReference"

	if kind == "" {
		kind = ReferenceNone
	}
	if !kind.Valid() {
		return DocumentReference{}, apperror.NewValidation(apperror.FieldError{
			Field: "reference_type", Rule: "oneof",
			Message: "reference type must be NONE, PURCHASE_ORDER or ASN",
		}).WithOp(op)
	}

	if kind == ReferenceNone {
		if id != nil && *id != uuid.Nil {
			return DocumentReference{}, apperror.NewValidation(apperror.FieldError{
				Field: "reference_id", Rule: "conflict",
				Message: "a receipt with no reference type cannot name a reference id",
			}).WithOp(op)
		}
		return DocumentReference{kind: ReferenceNone}, nil
	}

	if id == nil || *id == uuid.Nil {
		return DocumentReference{}, apperror.NewValidation(apperror.FieldError{
			Field: "reference_id", Rule: "required",
			Message: "a receipt referencing a " + kind.String() + " must name which one",
		}).WithOp(op)
	}

	copied := *id
	return DocumentReference{kind: kind, id: &copied}, nil
}

// Kind is the type of document referenced.
func (r DocumentReference) Kind() ReferenceType {
	if r.kind == "" {
		return ReferenceNone
	}
	return r.kind
}

// ID returns a COPY of the referenced document's id, or nil when there is none,
// so a caller cannot rewrite the value object through the returned pointer.
func (r DocumentReference) ID() *uuid.UUID {
	if r.id == nil {
		return nil
	}
	copied := *r.id
	return &copied
}

// HasReference reports whether a planning document was named.
func (r DocumentReference) HasReference() bool { return r.Kind() != ReferenceNone && r.id != nil }

// IsPurchaseOrder reports whether the receipt references a purchase order. It is
// what the application service consults before telling the order that its goods
// arrived.
func (r DocumentReference) IsPurchaseOrder() bool {
	return r.Kind() == ReferencePurchaseOrder && r.id != nil
}
