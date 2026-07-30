// Package dto holds the purchase-order module's transport contracts.
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

// LineRequest is one article on a purchase order.
//
// It carries no received quantity: receiving is a downstream event booked by the
// Goods Receipt flow, never something a client asserts while drafting.
type LineRequest struct {
	ProductID  uuid.UUID `json:"product_id"  binding:"required"`
	UOMID      uuid.UUID `json:"uom_id"      binding:"required"`
	OrderedQty int64     `json:"ordered_qty" binding:"required,gt=0"`

	// UnitPrice is in MINOR UNITS. A pointer so "not priced" stays distinct from
	// a price of zero.
	UnitPrice *int64 `json:"unit_price" binding:"omitempty,gte=0"`

	Remarks string `json:"remarks" binding:"omitempty,max=500"`
}

// CreatePurchaseOrderRequest drafts an order. Status is absent: an order is
// always created DRAFT and moves on only through the lifecycle endpoints.
type CreatePurchaseOrderRequest struct {
	Number string `json:"number" binding:"required,min=3,max=32"`

	SupplierID  uuid.UUID `json:"supplier_id"  binding:"required"`
	WarehouseID uuid.UUID `json:"warehouse_id" binding:"required"`

	OrderDate           time.Time `json:"order_date"            binding:"required"`
	ExpectedArrivalDate time.Time `json:"expected_arrival_date" binding:"required"`

	Remarks string `json:"remarks" binding:"omitempty,max=1000"`

	// Lines are optional at creation: a draft legitimately starts empty and is
	// filled in later.
	Lines []LineRequest `json:"lines" binding:"omitempty,dive"`
}

// UpdatePurchaseOrderRequest replaces the mutable attributes of a DRAFT.
//
// It is a FULL representation of the editable state — the client sends what it
// wants the order to be — because the line set is one value from the document's
// point of view and a partial update of it is ambiguous. Number is NOT updatable:
// it is printed on the document the supplier receives.
type UpdatePurchaseOrderRequest struct {
	SupplierID  uuid.UUID `json:"supplier_id"  binding:"required"`
	WarehouseID uuid.UUID `json:"warehouse_id" binding:"required"`

	OrderDate           time.Time `json:"order_date"            binding:"required"`
	ExpectedArrivalDate time.Time `json:"expected_arrival_date" binding:"required"`

	Remarks string `json:"remarks" binding:"omitempty,max=1000"`

	Lines []LineRequest `json:"lines" binding:"omitempty,dive"`
}

// CancelPurchaseOrderRequest carries the reason an order was called off.
type CancelPurchaseOrderRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=1000"`
}

// ---------- Queries ----------

// ListPurchaseOrdersQuery is the list endpoint's query string.
//
// The id filters bind as strings and are parsed after validation, because Gin's
// form binder rejects uuid.UUID (a [16]byte array).
type ListPurchaseOrdersQuery struct {
	pagination.Request

	Status      string `form:"status"       binding:"omitempty,oneof=DRAFT APPROVED PARTIALLY_RECEIVED COMPLETED CANCELLED"`
	SupplierID  string `form:"supplier_id"  binding:"omitempty,uuid"`
	WarehouseID string `form:"warehouse_id" binding:"omitempty,uuid"`

	// Half-open range over order_date: from inclusive, to exclusive.
	OrderedFrom string `form:"ordered_from" binding:"omitempty"`
	OrderedTo   string `form:"ordered_to"   binding:"omitempty"`
}

// ParseOrderedFrom parses the inclusive lower bound, or nil when absent.
func (q ListPurchaseOrdersQuery) ParseOrderedFrom() (*time.Time, error) {
	return parseTime("ordered_from", q.OrderedFrom)
}

// ParseOrderedTo parses the exclusive upper bound, or nil when absent.
func (q ListPurchaseOrdersQuery) ParseOrderedTo() (*time.Time, error) {
	return parseTime("ordered_to", q.OrderedTo)
}

// parseTime accepts RFC3339, or a bare date read as midnight UTC so a caller can
// write ?ordered_from=2026-08-01 without a clock component.
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
		Field:   field,
		Rule:    "datetime",
		Message: "must be an RFC3339 timestamp or a YYYY-MM-DD date",
	}).WithOp("purchaseorder.dto.parseTime")
}

// SortOptions declares this endpoint's paging rules. AllowedSorts is a security
// control: ORDER BY cannot be parameterised, so only keys listed here reach SQL.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "order_date",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"number":                "purchase_orders.number",
			"status":                "purchase_orders.status",
			"order_date":            "purchase_orders.order_date",
			"expected_arrival_date": "purchase_orders.expected_arrival_date",
			"created_at":            "purchase_orders.created_at",
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
			Field:   "id",
			Rule:    "uuid",
			Message: "id must be a valid UUID",
		}).WithOp("purchaseorder.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// LineResponse is the public representation of one order line.
type LineResponse struct {
	ID uuid.UUID `json:"id"`

	ProductID uuid.UUID `json:"product_id"`
	UOMID     uuid.UUID `json:"uom_id"`

	OrderedQty  int64 `json:"ordered_qty"`
	ReceivedQty int64 `json:"received_qty"`

	// RemainingQty is DERIVED by the aggregate and reported for convenience; it
	// is stored nowhere.
	RemainingQty int64 `json:"remaining_qty"`

	UnitPrice *int64 `json:"unit_price,omitempty"`
	Remarks   string `json:"remarks,omitempty"`
}

// PurchaseOrderResponse is the public representation of a purchase order.
type PurchaseOrderResponse struct {
	ID        uuid.UUID `json:"id"`
	CompanyID uuid.UUID `json:"company_id"`

	Number string `json:"number"`

	SupplierID  uuid.UUID `json:"supplier_id"`
	WarehouseID uuid.UUID `json:"warehouse_id"`

	OrderDate           time.Time `json:"order_date"`
	ExpectedArrivalDate time.Time `json:"expected_arrival_date"`

	Status  string `json:"status"`
	Remarks string `json:"remarks,omitempty"`

	Lines []LineResponse `json:"lines"`

	TotalOrderedQty  int64 `json:"total_ordered_qty"`
	TotalReceivedQty int64 `json:"total_received_qty"`

	// CanGenerateASN tells a client whether the "create ASN" action is available,
	// so the rule is stated once by the server rather than re-derived from the
	// status by every consumer.
	CanGenerateASN bool `json:"can_generate_asn"`

	CreatedBy  uuid.UUID  `json:"created_by"`
	ApprovedBy *uuid.UUID `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	UpdatedBy  uuid.UUID  `json:"updated_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
