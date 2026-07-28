// Package dto holds the customer module's transport contracts.
//
// LAYER RULE: DTOs are never entities. They carry validation tags and nothing
// else — the structural sibling of the supplier DTOs.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateCustomerRequest registers a customer. Status is absent: a customer is
// always created ACTIVE.
type CreateCustomerRequest struct {
	Code string `json:"code" binding:"required,min=2,max=32"`
	Name string `json:"name" binding:"required,min=2,max=255"`

	Email     string `json:"email"      binding:"omitempty,email,max=255"`
	Phone     string `json:"phone"      binding:"omitempty,min=3,max=32"`
	TaxNumber string `json:"tax_number" binding:"omitempty,max=64"`

	Address    string `json:"address"     binding:"omitempty,max=500"`
	City       string `json:"city"        binding:"omitempty,max=128"`
	Province   string `json:"province"    binding:"omitempty,max=128"`
	Country    string `json:"country"     binding:"omitempty,max=128"`
	PostalCode string `json:"postal_code" binding:"omitempty,max=16"`
}

// UpdateCustomerRequest replaces the mutable attributes. Full representation of
// the editable fields; the code is immutable.
type UpdateCustomerRequest struct {
	Name string `json:"name" binding:"required,min=2,max=255"`

	Email     string `json:"email"      binding:"omitempty,email,max=255"`
	Phone     string `json:"phone"      binding:"omitempty,min=3,max=32"`
	TaxNumber string `json:"tax_number" binding:"omitempty,max=64"`

	Address    string `json:"address"     binding:"omitempty,max=500"`
	City       string `json:"city"        binding:"omitempty,max=128"`
	Province   string `json:"province"    binding:"omitempty,max=128"`
	Country    string `json:"country"     binding:"omitempty,max=128"`
	PostalCode string `json:"postal_code" binding:"omitempty,max=16"`
}

// ListCustomersQuery is the list endpoint's query string.
type ListCustomersQuery struct {
	pagination.Request

	Status string `form:"status" binding:"omitempty,oneof=ACTIVE INACTIVE"`
}

// SortOptions declares this endpoint's paging rules.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "code",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{
			"code":       "customers.code",
			"name":       "customers.name",
			"status":     "customers.status",
			"created_at": "customers.created_at",
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
		}).WithOp("customer.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// CustomerResponse is the public representation of a customer.
type CustomerResponse struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`

	Email     string `json:"email,omitempty"`
	Phone     string `json:"phone,omitempty"`
	TaxNumber string `json:"tax_number,omitempty"`

	Address    string `json:"address,omitempty"`
	City       string `json:"city,omitempty"`
	Province   string `json:"province,omitempty"`
	Country    string `json:"country,omitempty"`
	PostalCode string `json:"postal_code,omitempty"`

	Status string `json:"status"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
