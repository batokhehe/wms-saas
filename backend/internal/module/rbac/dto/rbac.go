// Package dto holds the RBAC module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateRoleRequest defines a new custom role.
//
// IsSystem is deliberately absent. A client must not be able to mint a role
// that claims system protection — that would let anyone create an undeletable
// role, and system status is something the provisioner confers, never a caller.
type CreateRoleRequest struct {
	Name        string `json:"name"        binding:"required,min=2,max=32"`
	Description string `json:"description" binding:"omitempty,max=255"`

	// Permissions is optional. A role created with none is valid and grants
	// nothing until permissions are assigned — safer than inheriting a default
	// nobody chose.
	Permissions []string `json:"permissions" binding:"omitempty,max=128,dive,required,max=64"`
}

// UpdateRoleRequest applies a partial update.
//
// Name is NOT updatable, for either system or custom roles. memberships.role
// points at a role by NAME with no foreign key, so a rename would strand every
// member holding it — they would silently lose all permissions with no error
// anywhere. See entity.Role.CanRename.
type UpdateRoleRequest struct {
	Description *string `json:"description" binding:"omitempty,max=255"`
}

// SetRolePermissionsRequest replaces a role's entire permission set.
//
// Replace rather than add/remove: a client rendering a checkbox list knows the
// desired final state, not the delta. Making the API take that state directly
// removes a whole class of bug where the client computes the diff wrongly, and
// makes the operation idempotent.
//
// The service still computes the diff internally, so the audit trail records
// individual grants and revocations rather than one opaque "replaced".
type SetRolePermissionsRequest struct {
	// Permissions is the complete desired set. An empty array is meaningful and
	// valid: it revokes everything. It is `binding:"required"` so that OMITTING
	// the field is an error rather than being read as "revoke all" — a client
	// bug that sent `{}` would otherwise silently strip a role.
	Permissions []string `json:"permissions" binding:"required,max=128,dive,required,max=64"`
}

// ListRolesQuery is the role list query string.
type ListRolesQuery struct {
	pagination.Request

	// IsSystem filters to system or custom roles. A pointer so that omitting it
	// means "both" rather than "custom only".
	IsSystem *bool `form:"is_system"`
}

// RoleSortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control: ORDER BY cannot be parameterised by any
// SQL driver, so the column name is interpolated. Only keys listed here can
// ever reach the database.
func RoleSortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "name",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{
			"name":       "roles.name",
			"created_at": "roles.created_at",
		},
	}
}

// ListPermissionsQuery filters the global permission catalogue.
type ListPermissionsQuery struct {
	Module string `form:"module" binding:"omitempty,max=64"`
}

// IDParam binds a UUID path parameter.
//
// The field is a STRING, not a uuid.UUID. Gin's URI binder maps path segments
// by reflection over basic kinds; uuid.UUID is a [16]byte array, and the binder
// rejects it with "is not valid value for uuid.UUID" — producing a 400 on every
// request, including well-formed ones. Binding into a string and parsing after
// validation is the working shape. (Same reasoning as tenancy's IDParam.)
type IDParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

// UUID returns the parsed identifier.
//
// Safe after successful binding: the `uuid` tag has already validated the
// format. The error is returned rather than swallowed so a caller that skipped
// validation cannot silently proceed with uuid.Nil.
func (p IDParam) UUID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field:   "id",
			Rule:    "uuid",
			Message: "id must be a valid UUID",
		}).WithOp("rbac.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// PermissionResponse is one entry in the global catalogue.
type PermissionResponse struct {
	ID     uuid.UUID `json:"id"`
	Code   string    `json:"code"`
	Name   string    `json:"name"`
	Module string    `json:"module"`
}

// RoleResponse is the public representation of a role.
//
// Permissions are included inline. A role list without them forces the client
// into one request per role to render a permissions matrix, which is the N+1
// problem moved to the network.
type RoleResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MyPermissionsResponse reports what the caller may do in the active company.
//
// This is what a client uses to hide buttons the user cannot press. It is a
// convenience, never a security boundary: the server enforces every permission
// independently, and a client that ignored this response entirely would gain
// nothing.
type MyPermissionsResponse struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}
