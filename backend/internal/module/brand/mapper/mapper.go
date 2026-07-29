package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

func ToResponse(b *entity.Brand) dto.BrandResponse {
	return dto.BrandResponse{
		ID: b.ID(), Code: b.Code(), Name: b.Name(), Description: b.Description(),
		Status: b.Status().String(), CreatedAt: b.CreatedAt(), UpdatedAt: b.UpdatedAt(),
	}
}

func ToPage(page pagination.Page[*entity.Brand]) pagination.Page[dto.BrandResponse] {
	items := make([]dto.BrandResponse, len(page.Items))
	for i, item := range page.Items {
		items[i] = ToResponse(item)
	}
	return pagination.Page[dto.BrandResponse]{Items: items, Meta: page.Meta}
}
