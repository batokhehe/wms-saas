// Package dto holds the warehouse module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. Here that is
// especially clear — entity.Warehouse has no exported fields at all, so a DTO
// could not double as one even if someone wanted it to.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateWarehouseRequest registers a warehouse.
//
// Status is deliberately absent. A warehouse is always created in DRAFT and
// reaches ACTIVE only through the activation endpoint, which enforces the
// readiness rules. Letting a client choose the initial status would be a way to
// bypass them entirely.
//
// Address, contact and zones are absent for a different reason: they are
// supplied through their own intent-revealing operations. Accepting them here
// would make Create a general-purpose setter, which is exactly the CRUD shape
// this module is not.
type CreateWarehouseRequest struct {
	Code        string `json:"code"        binding:"required,min=2,max=32"`
	Name        string `json:"name"        binding:"required,min=2,max=255"`
	Description string `json:"description" binding:"omitempty,max=2000"`
	Type        string `json:"type"        binding:"required,oneof=MAIN BRANCH TRANSIT CONSIGNMENT"`
}

// UpdateWarehouseRequest applies a partial update.
//
// Pointer fields distinguish "field omitted" from "field set to empty", which
// matters here more than usual: clearing an address is a meaningful operation
// the aggregate refuses on an ACTIVE warehouse, and it must be distinguishable
// from not mentioning the address.
//
// Code is NOT updatable. It is printed on labels, embedded in transfer
// documents and read aloud over a radio, so changing it invalidates physical
// artefacts the system does not control.
//
// Status is NOT updatable either — status changes go through the lifecycle
// endpoints, which is what keeps the state machine in the aggregate rather than
// in whatever a client happened to send.
type UpdateWarehouseRequest struct {
	Name        *string `json:"name"        binding:"omitempty,min=2,max=255"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
	Address     *string `json:"address"     binding:"omitempty,max=2000"`

	// Zone assignments. Each is optional; supplying one assigns it.
	ReceivingZoneID *uuid.UUID `json:"receiving_zone_id"`
	ShippingZoneID  *uuid.UUID `json:"shipping_zone_id"`
	StagingZoneID   *uuid.UUID `json:"staging_zone_id"`
}

// ChangeContactRequest updates who is responsible for a warehouse.
//
// A dedicated endpoint rather than fields on the update request, because
// changing the contact raises a domain event that downstream notification
// routing needs — and burying it inside a general update would make "the
// contact changed" indistinguishable from "someone edited the description".
type ChangeContactRequest struct {
	ContactName  string `json:"contact_name"  binding:"required,min=1,max=255"`
	ContactPhone string `json:"contact_phone" binding:"required,min=3,max=32"`
}

// SuspendWarehouseRequest places a warehouse under a hold.
//
// The reason is required by the aggregate, not merely by this tag: a suspension
// nobody can explain is one nobody can safely lift.
type SuspendWarehouseRequest struct {
	Reason string `json:"reason" binding:"required,min=3,max=500"`
}

// ListWarehousesQuery is the list endpoint's query string.
type ListWarehousesQuery struct {
	pagination.Request

	Status string `form:"status" binding:"omitempty,oneof=DRAFT ACTIVE INACTIVE SUSPENDED"`
	Type   string `form:"type"   binding:"omitempty,oneof=MAIN BRANCH TRANSIT CONSIGNMENT"`
}

// SortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control: ORDER BY cannot be parameterised by any
// SQL driver, so the column name is interpolated. Only keys listed here can
// ever reach the database.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "code",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{
			"code":       "warehouses.code",
			"name":       "warehouses.name",
			"status":     "warehouses.status",
			"type":       "warehouses.type",
			"created_at": "warehouses.created_at",
		},
	}
}

// IDParam binds a UUID path parameter.
//
// The field is a STRING, not a uuid.UUID. Gin's URI binder maps path segments
// by reflection over basic kinds; uuid.UUID is a [16]byte array and the binder
// rejects it, producing a 400 on every request including well-formed ones.
// Binding into a string and parsing after validation is the working shape.
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
		}).WithOp("warehouse.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// ZonesResponse reports a warehouse's default operational zones.
type ZonesResponse struct {
	ReceivingZoneID *uuid.UUID `json:"receiving_zone_id,omitempty"`
	ShippingZoneID  *uuid.UUID `json:"shipping_zone_id,omitempty"`
	StagingZoneID   *uuid.UUID `json:"staging_zone_id,omitempty"`
}

// WarehouseResponse is the public representation of a warehouse.
//
// CanReceive and CanShip are computed by the AGGREGATE and reported here rather
// than left for a client to derive from the status. A client deriving them
// would be reimplementing a business rule, and the two would drift the moment
// the rule gains a condition — for example when receiving starts requiring a
// receiving zone.
type WarehouseResponse struct {
	ID           uuid.UUID     `json:"id"`
	Code         string        `json:"code"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Type         string        `json:"type"`
	Status       string        `json:"status"`
	Address      string        `json:"address,omitempty"`
	ContactName  string        `json:"contact_name,omitempty"`
	ContactPhone string        `json:"contact_phone,omitempty"`
	Zones        ZonesResponse `json:"zones"`

	CanReceive bool `json:"can_receive"`
	CanShip    bool `json:"can_ship"`

	IsArchived bool       `json:"is_archived"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
