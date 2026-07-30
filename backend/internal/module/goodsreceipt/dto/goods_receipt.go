// Package dto holds the goods-receipt module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. They carry
// validation tags and nothing else.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// LineRequest is one article arriving on a receipt.
type LineRequest struct {
	ProductID  uuid.UUID `json:"product_id"  binding:"required"`
	LocationID uuid.UUID `json:"location_id" binding:"required"`
	UOMID      uuid.UUID `json:"uom_id"      binding:"required"`
	Quantity   int64     `json:"quantity"    binding:"required,gt=0"`

	BatchNumber   string   `json:"batch_number"   binding:"omitempty,max=64"`
	LotNumber     string   `json:"lot_number"     binding:"omitempty,max=64"`
	SerialNumbers []string `json:"serial_numbers" binding:"omitempty,dive,max=128"`

	ExpiryDate *time.Time `json:"expiry_date"`
	Remarks    string     `json:"remarks" binding:"omitempty,max=500"`
}

// CreateGoodsReceiptRequest drafts a receipt. Status is absent: a receipt is
// always created DRAFT and moves on only through the lifecycle endpoints.
type CreateGoodsReceiptRequest struct {
	Number string `json:"number" binding:"required,min=3,max=32"`

	WarehouseID uuid.UUID  `json:"warehouse_id" binding:"required"`
	SupplierID  *uuid.UUID `json:"supplier_id"`

	// ReferenceType is NONE, PURCHASE_ORDER or ASN. Empty is read as NONE.
	ReferenceType string     `json:"reference_type" binding:"omitempty,oneof=NONE PURCHASE_ORDER ASN"`
	ReferenceID   *uuid.UUID `json:"reference_id"`

	ReceiptDate time.Time `json:"receipt_date" binding:"required"`
	Remarks     string    `json:"remarks" binding:"omitempty,max=1000"`

	Lines []LineRequest `json:"lines" binding:"omitempty,dive"`
}

// UpdateGoodsReceiptRequest replaces the mutable attributes of a DRAFT.
//
// A FULL representation of the editable state — the line set is one value from
// the document's point of view and a partial update of it is ambiguous. Number is
// NOT updatable: it is the document's external reference.
type UpdateGoodsReceiptRequest struct {
	WarehouseID uuid.UUID  `json:"warehouse_id" binding:"required"`
	SupplierID  *uuid.UUID `json:"supplier_id"`

	ReferenceType string     `json:"reference_type" binding:"omitempty,oneof=NONE PURCHASE_ORDER ASN"`
	ReferenceID   *uuid.UUID `json:"reference_id"`

	ReceiptDate time.Time `json:"receipt_date" binding:"required"`
	Remarks     string    `json:"remarks" binding:"omitempty,max=1000"`

	Lines []LineRequest `json:"lines" binding:"omitempty,dive"`
}

// CancelGoodsReceiptRequest carries the reason a receipt was abandoned.
type CancelGoodsReceiptRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=1000"`
}

// ---------- Queries ----------

// ListGoodsReceiptsQuery is the list endpoint's query string.
//
// The id filters bind as strings and are parsed after validation, because Gin's
// form binder rejects uuid.UUID (a [16]byte array).
type ListGoodsReceiptsQuery struct {
	pagination.Request

	Status      string `form:"status"       binding:"omitempty,oneof=DRAFT CONFIRMED RECEIVED CANCELLED"`
	WarehouseID string `form:"warehouse_id" binding:"omitempty,uuid"`
	SupplierID  string `form:"supplier_id"  binding:"omitempty,uuid"`

	ReferenceType string `form:"reference_type" binding:"omitempty,oneof=NONE PURCHASE_ORDER ASN"`
	ReferenceID   string `form:"reference_id"   binding:"omitempty,uuid"`

	// Half-open range over receipt_date: from inclusive, to exclusive.
	ReceivedFrom string `form:"received_from" binding:"omitempty"`
	ReceivedTo   string `form:"received_to"   binding:"omitempty"`
}

// ParseReceivedFrom parses the inclusive lower bound, or nil when absent.
func (q ListGoodsReceiptsQuery) ParseReceivedFrom() (*time.Time, error) {
	return parseTime("received_from", q.ReceivedFrom)
}

// ParseReceivedTo parses the exclusive upper bound, or nil when absent.
func (q ListGoodsReceiptsQuery) ParseReceivedTo() (*time.Time, error) {
	return parseTime("received_to", q.ReceivedTo)
}

// parseTime accepts RFC3339, or a bare date read as midnight UTC.
func parseTime(field, raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc, nil
		}
	}
	return nil, apperror.NewValidation(apperror.FieldError{
		Field: field, Rule: "datetime",
		Message: "must be an RFC3339 timestamp or a YYYY-MM-DD date",
	}).WithOp("goodsreceipt.dto.parseTime")
}

// SortOptions declares this endpoint's paging rules. AllowedSorts is a security
// control: ORDER BY cannot be parameterised, so only keys listed here reach SQL.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "receipt_date",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"number":       "goods_receipts.number",
			"status":       "goods_receipts.status",
			"receipt_date": "goods_receipts.receipt_date",
			"created_at":   "goods_receipts.created_at",
		},
	}
}

// IDParam binds a UUID path parameter as a string, then parses it.
type IDParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

// UUID returns the parsed identifier.
func (p IDParam) UUID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field: "id", Rule: "uuid",
			Message: "id must be a valid UUID",
		}).WithOp("goodsreceipt.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// LineResponse is the public representation of one receipt line.
type LineResponse struct {
	ID uuid.UUID `json:"id"`

	ProductID  uuid.UUID `json:"product_id"`
	LocationID uuid.UUID `json:"location_id"`
	UOMID      uuid.UUID `json:"uom_id"`

	Quantity int64 `json:"quantity"`

	BatchNumber   string   `json:"batch_number,omitempty"`
	LotNumber     string   `json:"lot_number,omitempty"`
	SerialNumbers []string `json:"serial_numbers,omitempty"`

	ExpiryDate *time.Time `json:"expiry_date,omitempty"`
	Remarks    string     `json:"remarks,omitempty"`
}

// GoodsReceiptResponse is the public representation of a goods receipt.
type GoodsReceiptResponse struct {
	ID        uuid.UUID `json:"id"`
	CompanyID uuid.UUID `json:"company_id"`

	Number string `json:"number"`

	WarehouseID uuid.UUID  `json:"warehouse_id"`
	SupplierID  *uuid.UUID `json:"supplier_id,omitempty"`

	ReferenceType string     `json:"reference_type"`
	ReferenceID   *uuid.UUID `json:"reference_id,omitempty"`

	ReceiptDate time.Time `json:"receipt_date"`
	Status      string    `json:"status"`
	Remarks     string    `json:"remarks,omitempty"`

	Lines []LineResponse `json:"lines"`

	TotalQuantity int64 `json:"total_quantity"`

	CreatedBy  uuid.UUID  `json:"created_by"`
	ReceivedBy *uuid.UUID `json:"received_by,omitempty"`
	UpdatedBy  uuid.UUID  `json:"updated_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
