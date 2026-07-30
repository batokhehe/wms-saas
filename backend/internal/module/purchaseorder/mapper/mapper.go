// Package mapper translates the PurchaseOrder aggregate into transport DTOs.
//
// LAYER RULE: one direction only — domain to DTO. Building an aggregate from a
// request is the SERVICE's job, because that direction has to validate, and
// validation is not translation.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse renders one purchase order.
func ToResponse(o *entity.PurchaseOrder) dto.PurchaseOrderResponse {
	lines := o.Lines()
	rendered := make([]dto.LineResponse, 0, len(lines))
	for _, line := range lines {
		var price *int64
		if !line.UnitPrice().IsZero() {
			amount := line.UnitPrice().Amount()
			price = &amount
		}
		rendered = append(rendered, dto.LineResponse{
			ID:           line.ID(),
			ProductID:    line.ProductID(),
			UOMID:        line.UOMID(),
			OrderedQty:   line.OrderedQty().Value(),
			ReceivedQty:  line.ReceivedQty().Value(),
			RemainingQty: line.RemainingQty().Value(),
			UnitPrice:    price,
			Remarks:      line.Remarks(),
		})
	}

	return dto.PurchaseOrderResponse{
		ID:                  o.ID(),
		CompanyID:           o.CompanyID(),
		Number:              o.Number().String(),
		SupplierID:          o.SupplierID(),
		WarehouseID:         o.WarehouseID(),
		OrderDate:           o.OrderDate(),
		ExpectedArrivalDate: o.ExpectedArrivalDate(),
		Status:              o.Status().String(),
		Remarks:             o.Remarks(),
		Lines:               rendered,
		TotalOrderedQty:     o.TotalOrderedQty(),
		TotalReceivedQty:    o.TotalReceivedQty(),
		CanGenerateASN:      o.CanGenerateASN(),
		CreatedBy:           o.CreatedBy(),
		ApprovedBy:          o.ApprovedBy(),
		ApprovedAt:          o.ApprovedAt(),
		UpdatedBy:           o.UpdatedBy(),
		CreatedAt:           o.CreatedAt(),
		UpdatedAt:           o.UpdatedAt(),
	}
}

// ToPage renders a page of purchase orders, preserving the paging metadata.
func ToPage(page pagination.Page[*entity.PurchaseOrder]) pagination.Page[dto.PurchaseOrderResponse] {
	items := make([]dto.PurchaseOrderResponse, 0, len(page.Items))
	for _, order := range page.Items {
		items = append(items, ToResponse(order))
	}
	return pagination.Page[dto.PurchaseOrderResponse]{Items: items, Meta: page.Meta}
}
