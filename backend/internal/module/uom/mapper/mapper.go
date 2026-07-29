package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

func ToResponse(uom *entity.UOM) dto.UOMResponse {
	return dto.UOMResponse{
		ID:          uom.ID(),
		Code:        uom.Code(),
		Name:        uom.Name(),
		Description: uom.Description(),
		Status:      string(uom.Status()),
		CreatedAt:   uom.CreatedAt(),
		UpdatedAt:   uom.UpdatedAt(),
	}
}

func ToPage(page pagination.Page[*entity.UOM]) pagination.Page[dto.UOMResponse] {
	items := make([]dto.UOMResponse, len(page.Items))
	for i, uom := range page.Items {
		items[i] = ToResponse(uom)
	}
	return pagination.Page[dto.UOMResponse]{
		Items: items,
		Meta:  page.Meta,
	}
}
