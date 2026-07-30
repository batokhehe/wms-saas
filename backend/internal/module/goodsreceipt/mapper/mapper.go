// Package mapper translates the GoodsReceipt aggregate into transport DTOs.
//
// LAYER RULE: one direction only — domain to DTO. Building an aggregate from a
// request is the SERVICE's job, because that direction has to validate.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse renders one goods receipt.
func ToResponse(g *entity.GoodsReceipt) dto.GoodsReceiptResponse {
	lines := g.Lines()
	rendered := make([]dto.LineResponse, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, dto.LineResponse{
			ID:            line.ID(),
			ProductID:     line.ProductID(),
			LocationID:    line.LocationID(),
			UOMID:         line.UOMID(),
			Quantity:      line.Quantity().Value(),
			BatchNumber:   line.BatchNumber(),
			LotNumber:     line.LotNumber(),
			SerialNumbers: line.SerialNumbers(),
			ExpiryDate:    line.ExpiryDate(),
			Remarks:       line.Remarks(),
		})
	}

	return dto.GoodsReceiptResponse{
		ID:            g.ID(),
		CompanyID:     g.CompanyID(),
		Number:        g.Number().String(),
		WarehouseID:   g.WarehouseID(),
		SupplierID:    g.SupplierID(),
		ReferenceType: g.Reference().Kind().String(),
		ReferenceID:   g.Reference().ID(),
		ReceiptDate:   g.ReceiptDate(),
		Status:        g.Status().String(),
		Remarks:       g.Remarks(),
		Lines:         rendered,
		TotalQuantity: g.TotalQuantity(),
		CreatedBy:     g.CreatedBy(),
		ReceivedBy:    g.ReceivedBy(),
		UpdatedBy:     g.UpdatedBy(),
		CreatedAt:     g.CreatedAt(),
		UpdatedAt:     g.UpdatedAt(),
	}
}

// ToPage renders a page of goods receipts, preserving the paging metadata.
func ToPage(page pagination.Page[*entity.GoodsReceipt]) pagination.Page[dto.GoodsReceiptResponse] {
	items := make([]dto.GoodsReceiptResponse, 0, len(page.Items))
	for _, receipt := range page.Items {
		items = append(items, ToResponse(receipt))
	}
	return pagination.Page[dto.GoodsReceiptResponse]{Items: items, Meta: page.Meta}
}
