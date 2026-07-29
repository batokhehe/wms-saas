package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

func ToResponse(p *entity.Product) dto.ProductResponse {
	return dto.ProductResponse{
		ID: p.ID(), SKU: string(p.SKU()), Name: string(p.Name()), Description: p.Description(),
		Status: p.Status().String(), CreatedAt: p.CreatedAt(), UpdatedAt: p.UpdatedAt(),
	}
}

func ToPage(page pagination.Page[*entity.Product]) pagination.Page[dto.ProductResponse] {
	items := make([]dto.ProductResponse, len(page.Items))
	for i, item := range page.Items {
		items[i] = ToResponse(item)
	}
	return pagination.Page[dto.ProductResponse]{Items: items, Meta: page.Meta}
}
