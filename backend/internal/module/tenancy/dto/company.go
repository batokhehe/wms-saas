// Package dto holds the tenancy module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. They change
// for different reasons — a DTO when the API contract changes, an entity when
// the domain does.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateCompanyRequest onboards a new tenant.
//
// Status is deliberately absent. A client must not be able to create a company
// that is already SUSPENDED, and allowing it to choose ACTIVE would imply the
// choice was ever theirs. New companies are always ACTIVE; changing that is an
// administrative action.
type CreateCompanyRequest struct {
	Code    string `json:"code"    binding:"required,min=2,max=32,alphanum"`
	Name    string `json:"name"    binding:"required,min=1,max=255"`
	Email   string `json:"email"   binding:"omitempty,email,max=255"`
	Phone   string `json:"phone"   binding:"omitempty,max=32"`
	Address string `json:"address" binding:"omitempty,max=2000"`
	Logo    string `json:"logo"    binding:"omitempty,max=512"`
}

// UpdateCompanyRequest applies a partial update.
//
// Pointer fields distinguish "field omitted" from "field set to empty", which a
// PUT/PATCH endpoint must be able to tell apart — otherwise clearing a phone
// number is indistinguishable from not mentioning it.
//
// Code is NOT updatable. It appears on printed documents, in support tickets
// and in external integrations, so changing it silently invalidates references
// the system does not control. A rename would need a deliberate migration flow.
type UpdateCompanyRequest struct {
	Name    *string `json:"name"    binding:"omitempty,min=1,max=255"`
	Email   *string `json:"email"   binding:"omitempty,email,max=255"`
	Phone   *string `json:"phone"   binding:"omitempty,max=32"`
	Address *string `json:"address" binding:"omitempty,max=2000"`
	Logo    *string `json:"logo"    binding:"omitempty,max=512"`
	Status  *string `json:"status"  binding:"omitempty,oneof=ACTIVE INACTIVE SUSPENDED"`
}

// SwitchCompanyRequest changes the caller's active tenant.
type SwitchCompanyRequest struct {
	CompanyID uuid.UUID `json:"company_id" binding:"required"`
}

// ListCompaniesQuery is the company list query string.
//
// It embeds the shared pagination.Request so every list endpoint in the system
// accepts identical parameter names.
type ListCompaniesQuery struct {
	pagination.Request

	// Status filters the list. Constrained by tag so an arbitrary value cannot
	// reach the query builder.
	Status string `form:"status" binding:"omitempty,oneof=ACTIVE INACTIVE SUSPENDED"`
}

// CompanySortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control, not a convenience: ORDER BY cannot be
// parameterised by any SQL driver, so the column name is interpolated. Only
// keys listed here can ever reach the database.
func CompanySortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"code":       "companies.code",
			"name":       "companies.name",
			"status":     "companies.status",
			"created_at": "companies.created_at",
		},
	}
}

// CompanySearchColumns lists the columns a `search` term matches against.
// Compile-time constants, never derived from client input.
func CompanySearchColumns() []string {
	return []string{"companies.code", "companies.name"}
}

// IDParam binds a UUID path parameter.
//
// The field is a STRING, not a uuid.UUID, and that is not a style choice.
// Gin's URI binder maps path segments by reflection over basic kinds; uuid.UUID
// is a [16]byte array, and the binder rejects it with
//
//	["<the id>"] is not valid value for uuid.UUID
//
// producing a 400 on every request — including well-formed ones. Binding into a
// string and parsing after validation is the working shape.
//
// The `uuid` binding tag still runs, so a malformed id is a clean 422 with
// field details before UUID() is ever called.
type IDParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

// UUID returns the parsed identifier.
//
// Safe after successful binding: the `uuid` tag has already validated the
// format. The error is returned rather than swallowed so a caller that skipped
// validation cannot silently proceed with uuid.Nil — which would read as
// "the zero company" rather than "no company".
func (p IDParam) UUID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field:   "id",
			Rule:    "uuid",
			Message: "id must be a valid UUID",
		}).WithOp("tenancy.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// CompanyResponse is the public representation of a tenant.
type CompanyResponse struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Logo      string    `json:"logo,omitempty"`
	Address   string    `json:"address,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CurrentCompanyResponse describes the caller's active tenant AND their
// standing within it.
//
// The two are returned together because a client rendering a header needs both
// — the company name and what the user is allowed to do — and splitting them
// across two endpoints would make every page load two round trips.
type CurrentCompanyResponse struct {
	Company      CompanyResponse `json:"company"`
	MembershipID uuid.UUID       `json:"membership_id"`
	Role         string          `json:"role"`
}
