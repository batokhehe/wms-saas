// Package dto holds the inventory module's transport contracts.
//
// LAYER RULE: DTOs are never entities. entity.InventoryPosition has no exported
// fields, so a DTO could not double as one even if someone wanted it to.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Stock key ----------

// StockKeyRequest addresses a position: where it is, what it is, and how it is
// individuated. It is embedded by the operations that may have to OPEN a
// position, which need the full key rather than an id.
type StockKeyRequest struct {
	WarehouseID uuid.UUID `json:"warehouse_id" binding:"required"`
	LocationID  uuid.UUID `json:"location_id"  binding:"required"`
	ProductID   uuid.UUID `json:"product_id"   binding:"required"`

	Tracking     string  `json:"tracking"      binding:"required,oneof=NONE LOT SERIAL"`
	LotNumber    *string `json:"lot_number"    binding:"omitempty,max=64"`
	SerialNumber *string `json:"serial_number" binding:"omitempty,max=128"`
}

// ---------- Requests ----------

// ReceiveStockRequest books stock into a position, opening it on first receipt.
type ReceiveStockRequest struct {
	StockKeyRequest
	Quantity int64 `json:"quantity" binding:"required,gt=0"`
}

// PositionQuantityRequest is the shape of every operation that moves a quantity
// within an existing position: issue, reserve, release, allocate, deallocate and
// both quarantine directions.
type PositionQuantityRequest struct {
	PositionID uuid.UUID `json:"position_id" binding:"required"`
	Quantity   int64     `json:"quantity"    binding:"required,gt=0"`
}

// TransferStockRequest moves stock between two positions. The destination is
// opened if it does not exist; both sides commit atomically.
type TransferStockRequest struct {
	FromPositionID uuid.UUID `json:"from_position_id" binding:"required"`

	ToWarehouseID uuid.UUID `json:"to_warehouse_id" binding:"required"`
	ToLocationID  uuid.UUID `json:"to_location_id"  binding:"required"`

	Quantity int64 `json:"quantity" binding:"required,gt=0"`
}

// AdjustmentType names WHY an absolute stock correction was made.
//
// The reason is not decoration: a cycle count and a shrinkage write-off move the
// same number of units but mean opposite things to finance and to the auditor
// asking why a position changed. A closed enum lets a report group by it.
type AdjustmentType string

const (
	// AdjustmentCycleCount reconciles to a physical count.
	AdjustmentCycleCount AdjustmentType = "CYCLE_COUNT"

	// AdjustmentDamage writes off units that can no longer be sold.
	AdjustmentDamage AdjustmentType = "DAMAGE"

	// AdjustmentShrinkage writes off units lost to theft or unexplained loss.
	AdjustmentShrinkage AdjustmentType = "SHRINKAGE"

	// AdjustmentFound records units discovered that the system did not know of.
	AdjustmentFound AdjustmentType = "FOUND"

	// AdjustmentInitialBalance seeds an opening balance at go-live.
	AdjustmentInitialBalance AdjustmentType = "INITIAL_BALANCE"

	// AdjustmentManualCorrection is an operator correction with a written reason.
	AdjustmentManualCorrection AdjustmentType = "MANUAL_CORRECTION"
)

// Valid reports whether the adjustment type is a known value.
func (a AdjustmentType) Valid() bool {
	switch a {
	case AdjustmentCycleCount, AdjustmentDamage, AdjustmentShrinkage,
		AdjustmentFound, AdjustmentInitialBalance, AdjustmentManualCorrection:
		return true
	default:
		return false
	}
}

// String renders the adjustment type.
func (a AdjustmentType) String() string { return string(a) }

// AdjustStockRequest reconciles a position to a counted total. Quantity is a
// pointer so an adjustment to zero (a full write-off) is distinguishable from an
// omitted field.
type AdjustStockRequest struct {
	PositionID uuid.UUID      `json:"position_id" binding:"required"`
	Quantity   *int64         `json:"quantity"    binding:"required,gte=0"`
	Type       AdjustmentType `json:"type"        binding:"required,oneof=CYCLE_COUNT DAMAGE SHRINKAGE FOUND INITIAL_BALANCE MANUAL_CORRECTION"`
	Reason     string         `json:"reason"      binding:"omitempty,max=500"`
}

// ListPositionsQuery is the position-listing query string. The id filters bind as
// strings and are parsed after validation, because Gin's form binder rejects
// uuid.UUID (a [16]byte array).
type ListPositionsQuery struct {
	pagination.Request

	WarehouseID string `form:"warehouse_id" binding:"omitempty,uuid"`
	LocationID  string `form:"location_id"  binding:"omitempty,uuid"`
	ProductID   string `form:"product_id"   binding:"omitempty,uuid"`
	Tracking    string `form:"tracking"     binding:"omitempty,oneof=NONE LOT SERIAL"`
}

// SortOptions declares this endpoint's paging rules. AllowedSorts is a security
// control: ORDER BY cannot be parameterised, so only keys listed here reach SQL.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"created_at": "inventory_positions.created_at",
			"updated_at": "inventory_positions.updated_at",
			"available":  "inventory_positions.available",
			"reserved":   "inventory_positions.reserved",
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
		}).WithOp("inventory.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// PositionResponse is the public representation of a stock position.
//
// All four buckets are reported alongside the derived OnHand, because a caller
// deciding whether it may take stock needs to know not just how much exists but
// how much is spoken for — and OnHand alone cannot answer that.
type PositionResponse struct {
	ID          uuid.UUID `json:"id"`
	CompanyID   uuid.UUID `json:"company_id"`
	WarehouseID uuid.UUID `json:"warehouse_id"`
	LocationID  uuid.UUID `json:"location_id"`
	ProductID   uuid.UUID `json:"product_id"`

	Tracking     string  `json:"tracking"`
	LotNumber    *string `json:"lot_number,omitempty"`
	SerialNumber *string `json:"serial_number,omitempty"`

	Available   int64 `json:"available"`
	Reserved    int64 `json:"reserved"`
	Allocated   int64 `json:"allocated"`
	Quarantined int64 `json:"quarantined"`
	OnHand      int64 `json:"on_hand"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
