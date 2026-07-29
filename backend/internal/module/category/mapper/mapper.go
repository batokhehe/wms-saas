package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/category/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

func ToResponse(c *entity.Category) dto.CategoryResponse {
	return dto.CategoryResponse{
		ID: c.ID(), Code: c.Code(), Name: c.Name(), Description: c.Description(),
		Status: c.Status().String(), CreatedAt: c.CreatedAt(), UpdatedAt: c.UpdatedAt(),
	}
}

func ToPage(page pagination.Page[*entity.Category]) pagination.Page[dto.CategoryResponse] {
	items := make([]dto.CategoryResponse, len(page.Items))
	for i, item := range page.Items {
		items[i] = ToResponse(item)
	}
	return pagination.Page[dto.CategoryResponse]{Items: items, Meta: page.Meta}
}
