// Package service orchestrates the Supplier aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES beyond the
// set-level ones an aggregate cannot see. The service loads an aggregate, runs a
// specification where a rule spans the whole company set, calls one domain
// method, persists and publishes.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// UniqueSupplierCode enforces "a supplier code identifies at most one live
// supplier per company". It is a first-class rule object over the repository, so
// the rule has a name the reviewer and the error message share, and the DB
// unique index remains the race-proof backstop behind it.
type UniqueSupplierCode struct{ suppliers repository.Repository }

// NewUniqueSupplierCode builds the specification.
func NewUniqueSupplierCode(suppliers repository.Repository) UniqueSupplierCode {
	return UniqueSupplierCode{suppliers: suppliers}
}

// Satisfy returns CONFLICT when the code is already taken in the company.
func (s UniqueSupplierCode) Satisfy(ctx context.Context, companyID uuid.UUID, code string) error {
	taken, err := s.suppliers.ExistsByCode(ctx, companyID, code)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A supplier with this code already exists").
			WithOp("supplier.spec.UniqueSupplierCode")
	}
	return nil
}
