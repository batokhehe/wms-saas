// Package dto holds the module's transport contracts: what the API accepts and
// what it returns.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. They change
// for different reasons — a DTO changes when the API contract changes, an
// entity when the domain does. Returning entities directly would mean every
// internal field rename becomes a breaking API change, and every new internal
// column silently leaks to clients.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// CreateRequest is the create-endpoint body.
//
// Validation lives in binding tags so it is declared next to the field it
// governs, and so the shared validator can produce uniform per-field errors.
type CreateRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

// UpdateRequest is the update-endpoint body.
//
// Pointer fields distinguish "field omitted" from "field set to empty", which a
// PATCH endpoint must be able to tell apart.
type UpdateRequest struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=255"`
}

// ListQuery is the list-endpoint query string.
//
// It embeds the shared pagination.Request rather than redeclaring page/limit/
// search/sort/order, so every list endpoint in the system accepts identical
// parameter names. Module-specific filters are added as extra fields alongside.
type ListQuery struct {
	pagination.Request

	// Module-specific filters go here, for example:
	//   Status string `form:"status" binding:"omitempty,oneof=active archived"`
}

// SortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control, not a convenience. `ORDER BY` cannot be
// parameterised by any SQL driver, so the column name is interpolated into the
// query — this map is what stops a client choosing that string. Only keys
// listed here can ever reach the database.
//
// The map also decouples the public API from the schema: clients sort by a
// stable name while the column behind it can be renamed or table-qualified.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"name":       "resources.name",
			"created_at": "resources.created_at",
			"updated_at": "resources.updated_at",
		},
	}
}

// SearchColumns lists the columns a `search` term matches against.
//
// They are compile-time constants and never derived from client input; only the
// search *term* is parameterised.
func SearchColumns() []string {
	return []string{"resources.name"}
}

// IDParam binds a UUID path parameter.
type IDParam struct {
	ID uuid.UUID `uri:"id" binding:"required,uuid"`
}

// Response is what the API returns for a single resource.
//
// Note the absence of CompanyID: the tenant is implied by the caller's token
// and echoing it back only widens what a client can learn.
type Response struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
