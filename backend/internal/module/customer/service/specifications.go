// Package service orchestrates the Customer aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES beyond the
// set-level ones an aggregate cannot see.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// UniqueCustomerCode enforces "a customer code identifies at most one live
// customer per company". A first-class rule object over the repository; the DB
// unique index is the race-proof backstop behind it.
type UniqueCustomerCode struct{ customers repository.Repository }

// NewUniqueCustomerCode builds the specification.
func NewUniqueCustomerCode(customers repository.Repository) UniqueCustomerCode {
	return UniqueCustomerCode{customers: customers}
}

// Satisfy returns CONFLICT when the code is already taken in the company.
func (s UniqueCustomerCode) Satisfy(ctx context.Context, companyID uuid.UUID, code string) error {
	taken, err := s.customers.ExistsByCode(ctx, companyID, code)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A customer with this code already exists").
			WithOp("customer.spec.UniqueCustomerCode")
	}
	return nil
}
