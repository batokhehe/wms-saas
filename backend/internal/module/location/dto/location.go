// Package dto holds the location module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. Here that is
// especially clear — entity.StorageLocation has no exported fields at all.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateLocationRequest defines a location.
//
// Status is deliberately absent: a location is always created ACTIVE, and the
// lifecycle endpoints move it from there. Letting a client choose the initial
// status would be a way to create a LOCKED location with no reason recorded.
//
// Code is optional. Omitted, it is derived from the coordinate — so
// "A-01-02-03" becomes both the address and the label with nothing to keep in
// sync. Supplied, it overrides, which is what a receiving dock labelled
// "DOCK-1" needs.
type CreateLocationRequest struct {
	WarehouseID uuid.UUID `json:"warehouse_id" binding:"required"`

	Code string `json:"code" binding:"omitempty,max=64"`

	Zone  string `json:"zone"  binding:"required,max=32"`
	Aisle string `json:"aisle" binding:"omitempty,max=32"`
	Rack  string `json:"rack"  binding:"omitempty,max=32"`
	Level string `json:"level" binding:"omitempty,max=32"`
	Bin   string `json:"bin"   binding:"omitempty,max=32"`

	Barcode string `json:"barcode" binding:"omitempty,max=64"`

	PickingPriority *int  `json:"picking_priority" binding:"omitempty,min=0"`
	AllowMixedSKU   *bool `json:"allow_mixed_sku"`
	AllowOverflow   *bool `json:"allow_overflow"`

	// Capacity limits are decimal STRINGS, not floats.
	//
	// JSON numbers are IEEE 754 doubles in every mainstream parser, so a
	// float64 field would silently round "1234.567" before the server ever
	// validated it. A string reaches the domain's big.Rat intact.
	MaxWeight string `json:"max_weight" binding:"omitempty,max=24"`
	MaxVolume string `json:"max_volume" binding:"omitempty,max=24"`
	MaxPallet *int   `json:"max_pallet" binding:"omitempty,min=0"`
}

// UpdateLocationRequest applies a partial update.
//
// Pointer and empty-string sentinels distinguish "field omitted" from "field
// set to empty", which matters here: clearing a barcode is a real operation,
// and it must be distinguishable from not mentioning the barcode.
//
// WarehouseID and the coordinate are NOT updatable. A location IS its physical
// place — moving a bin to a different aisle is not an edit, it is retiring one
// location and creating another, and pretending otherwise would silently
// invalidate every historical stock movement that referenced it.
//
// Status is not updatable either; transitions go through the lifecycle
// endpoints, which is what keeps the state machine in the aggregate.
type UpdateLocationRequest struct {
	PickingPriority *int  `json:"picking_priority" binding:"omitempty,min=0"`
	AllowMixedSKU   *bool `json:"allow_mixed_sku"`
	AllowOverflow   *bool `json:"allow_overflow"`
}

// AssignBarcodeRequest attaches or replaces a scannable label.
//
// A dedicated endpoint rather than a field on the update request, because
// assigning a barcode raises a domain event: a replaced label means an old one
// is still physically on the rack and must be removed. Burying that in a
// general update would make it indistinguishable from a priority change.
//
// An empty barcode clears the assignment.
type AssignBarcodeRequest struct {
	Barcode string `json:"barcode" binding:"omitempty,max=64"`
}

// ChangeCapacityRequest updates what a location can hold.
//
// A dedicated endpoint because the aggregate's central rule applies: capacity
// may not be reduced below what is currently stored, and the service must fetch
// that usage before calling the domain. An empty string clears a limit, meaning
// "not measured" — which is a widening and always permitted.
type ChangeCapacityRequest struct {
	MaxWeight string `json:"max_weight" binding:"omitempty,max=24"`
	MaxVolume string `json:"max_volume" binding:"omitempty,max=24"`
	MaxPallet *int   `json:"max_pallet" binding:"omitempty,min=0"`
}

// LockLocationRequest places a location under an operational hold.
//
// The reason is required by the aggregate, not merely by this tag: a lock
// nobody can explain is one nobody can safely lift, and the person lifting it
// is usually not the person who imposed it.
type LockLocationRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=500"`
}

// ListLocationsQuery is the list endpoint's query string.
type ListLocationsQuery struct {
	pagination.Request

	// WarehouseID scopes the listing to one site. Optional — a barcode audit
	// legitimately spans the whole company — but supplied for every operational
	// read.
	WarehouseID *uuid.UUID `form:"warehouse_id"`

	Status string `form:"status" binding:"omitempty,oneof=ACTIVE INACTIVE LOCKED MAINTENANCE"`
	Zone   string `form:"zone"   binding:"omitempty,max=32"`
}

// SortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control: ORDER BY cannot be parameterised by any
// SQL driver, so the column name is interpolated. Only keys listed here can
// ever reach the database.
//
// The default is picking_priority then code, which is the order a picker walks —
// making the unfiltered list immediately useful rather than arbitrary.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 50,
		MaxLimit:     200,
		DefaultSort:  "priority",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{
			"priority":   "storage_locations.picking_priority",
			"code":       "storage_locations.code",
			"zone":       "storage_locations.zone",
			"status":     "storage_locations.status",
			"created_at": "storage_locations.created_at",
		},
	}
}

// IDParam binds a UUID path parameter.
//
// The field is a STRING, not a uuid.UUID. Gin's URI binder maps path segments
// by reflection over basic kinds; uuid.UUID is a [16]byte array and the binder
// rejects it, producing a 400 on every request including well-formed ones.
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
		}).WithOp("location.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// CoordinateResponse reports a location's physical address.
type CoordinateResponse struct {
	Zone  string `json:"zone"`
	Aisle string `json:"aisle,omitempty"`
	Rack  string `json:"rack,omitempty"`
	Level string `json:"level,omitempty"`
	Bin   string `json:"bin,omitempty"`

	// Depth is how many segments are populated: 1 is a zone-level place such as
	// a dock, 5 is a fully specified bin. Reported so a client can render the
	// right level of detail without counting empty strings.
	Depth int `json:"depth"`
}

// CapacityResponse reports a location's limits.
//
// Every field is a string or a pointer, and absent means "not measured" — which
// is genuinely different from zero: an unmeasured bin accepts stock, a
// zero-capacity bin accepts none.
type CapacityResponse struct {
	MaxWeight string `json:"max_weight,omitempty"`
	MaxVolume string `json:"max_volume,omitempty"`
	MaxPallet *int   `json:"max_pallet,omitempty"`

	IsUnlimited bool `json:"is_unlimited"`
}

// LocationResponse is the public representation of a storage location.
//
// CanReceive and CanPick are computed by the AGGREGATE and reported here rather
// than left for a client to derive from the status. A client deriving them
// would be reimplementing a business rule — and would get it wrong, because the
// two differ: a MAINTENANCE location may be picked from but not received into.
type LocationResponse struct {
	ID          uuid.UUID `json:"id"`
	WarehouseID uuid.UUID `json:"warehouse_id"`

	Code       string             `json:"code"`
	Coordinate CoordinateResponse `json:"coordinate"`
	Barcode    string             `json:"barcode,omitempty"`
	Status     string             `json:"status"`

	PickingPriority int              `json:"picking_priority"`
	AllowMixedSKU   bool             `json:"allow_mixed_sku"`
	AllowOverflow   bool             `json:"allow_overflow"`
	Capacity        CapacityResponse `json:"capacity"`

	CanReceive bool `json:"can_receive"`
	CanPick    bool `json:"can_pick"`

	IsArchived bool       `json:"is_archived"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
