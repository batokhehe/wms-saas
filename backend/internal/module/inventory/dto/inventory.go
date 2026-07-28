// Package dto holds the inventory module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. Here that is
// especially clear — entity.Inventory has no exported fields at all, so a DTO
// could not double as one even if someone wanted it to. DTOs carry validation
// tags and nothing else; no business logic lives here.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateInventoryRequest opens a stock position for a product in a location.
//
// Status is absent: a position is always created ACTIVE. tracking fixes how the
// stock is individuated, and lot_number / serial_number are required or
// forbidden accordingly — the aggregate enforces which, this only carries them.
type CreateInventoryRequest struct {
	WarehouseID  uuid.UUID `json:"warehouse_id"  binding:"required"`
	LocationID   uuid.UUID `json:"location_id"   binding:"required"`
	ProductID    uuid.UUID `json:"product_id"    binding:"required"`
	Tracking     string    `json:"tracking"      binding:"required,oneof=NONE LOT SERIAL"`
	LotNumber    *string   `json:"lot_number"    binding:"omitempty,max=64"`
	SerialNumber *string   `json:"serial_number" binding:"omitempty,max=128"`

	// Quantity is the opening on-hand count. It may be zero (an empty position),
	// and must be exactly one for SERIAL — a rule the aggregate enforces.
	Quantity int64 `json:"quantity" binding:"gte=0"`
}

// QuantityRequest is a movement amount for increase, decrease, reserve, release
// and both transfer directions. The amount must be strictly positive; a
// zero-amount movement is a no-op the aggregate rejects.
type QuantityRequest struct {
	Quantity int64 `json:"quantity" binding:"required,gt=0"`
}

// AdjustInventoryRequest sets on-hand to an absolute corrected value.
//
// Quantity is a pointer so an adjustment to zero (a full write-off) is
// distinguishable from an omitted field — the target is required, and zero is a
// meaningful target.
type AdjustInventoryRequest struct {
	Quantity *int64 `json:"quantity" binding:"required,gte=0"`
	Reason   string `json:"reason"   binding:"omitempty,max=500"`
}

// CycleCountRequest reconciles on-hand to a physically counted value.
type CycleCountRequest struct {
	Counted *int64 `json:"counted" binding:"required,gte=0"`
}

// ListInventoriesQuery is the list endpoint's query string. The id filters are
// bound as strings and parsed after validation, because Gin's form binder
// rejects uuid.UUID (a [16]byte array) the same way it does on path parameters.
type ListInventoriesQuery struct {
	pagination.Request

	WarehouseID string `form:"warehouse_id" binding:"omitempty,uuid"`
	LocationID  string `form:"location_id"  binding:"omitempty,uuid"`
	ProductID   string `form:"product_id"   binding:"omitempty,uuid"`
	Tracking    string `form:"tracking"     binding:"omitempty,oneof=NONE LOT SERIAL"`
	Status      string `form:"status"       binding:"omitempty,oneof=ACTIVE LOCKED"`
}

// SortOptions declares this endpoint's paging rules. AllowedSorts is a security
// control: ORDER BY cannot be parameterised, so only keys listed here can ever
// reach the database.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"created_at": "inventories.created_at",
			"updated_at": "inventories.updated_at",
			"on_hand":    "inventories.on_hand",
			"reserved":   "inventories.reserved",
		},
	}
}

// IDParam binds a UUID path parameter as a string, then parses it — the working
// shape, because Gin's URI binder rejects uuid.UUID directly.
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
		}).WithOp("inventory.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// InventoryResponse is the public representation of a stock position.
//
// Available is reported by the AGGREGATE rather than left for a client to derive
// from on-hand and reserved: a client deriving it would be reimplementing a
// domain rule, and the two would drift the moment the derivation gains a nuance.
type InventoryResponse struct {
	ID          uuid.UUID `json:"id"`
	CompanyID   uuid.UUID `json:"company_id"`
	WarehouseID uuid.UUID `json:"warehouse_id"`
	LocationID  uuid.UUID `json:"location_id"`
	ProductID   uuid.UUID `json:"product_id"`

	Tracking string `json:"tracking"`
	Status   string `json:"status"`

	OnHand    int64 `json:"on_hand"`
	Reserved  int64 `json:"reserved"`
	Available int64 `json:"available"`

	LotNumber    *string `json:"lot_number,omitempty"`
	SerialNumber *string `json:"serial_number,omitempty"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
