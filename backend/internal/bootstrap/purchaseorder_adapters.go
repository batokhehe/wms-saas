package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	purchaseorderservice "github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/service"
	supplierrepo "github.com/batokhehe/wms-saas/backend/internal/module/supplier/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// This file joins the purchase-order module to supplier, without purchaseorder
// importing it.
//
// PurchaseOrder declares the narrow verifier interfaces it needs
// (purchaseorder/service/interfaces.go); bootstrap — the composition root, the
// one place permitted to know every module — supplies implementations over the
// other modules' repositories. Only the ANSWER crosses the boundary (an error or
// nil), never a foreign aggregate.
//
// The warehouse and product verifiers are NOT redeclared here: the adapters in
// inventory_adapters.go have identical method sets, and Go interfaces are
// structural, so the same instances satisfy both modules' contracts.

// ---------- purchaseorder → supplier ----------

type purchaseOrderSupplierProvider struct {
	suppliers supplierrepo.Repository
}

var _ purchaseorderservice.SupplierVerifier = (*purchaseOrderSupplierProvider)(nil)

func newPurchaseOrderSupplierProvider(suppliers supplierrepo.Repository) *purchaseOrderSupplierProvider {
	return &purchaseOrderSupplierProvider{suppliers: suppliers}
}

// VerifySupplier confirms a supplier exists in the company AND is active.
//
// The active check is the point of supplier deactivation: a deactivated supplier
// is one the company has decided to stop buying from, and the only thing that
// enforces that decision is refusing to place new orders with them. Existence
// alone would let a deactivated supplier keep receiving orders indefinitely.
func (p *purchaseOrderSupplierProvider) VerifySupplier(ctx context.Context, companyID, supplierID uuid.UUID) error {
	const op = "bootstrap.purchaseOrderSupplierProvider.VerifySupplier"

	supplier, err := p.suppliers.FindByID(ctx, supplierID, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return apperror.NewValidation(apperror.FieldError{
				Field: "supplier_id", Rule: "not_found",
				Message: "no supplier with this id exists in this company",
			}).WithOp(op)
		}
		return err
	}
	if !supplier.IsActive() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "supplier_id", Rule: "inactive",
			Message: "this supplier is inactive and cannot receive new purchase orders",
		}).WithOp(op)
	}
	return nil
}
