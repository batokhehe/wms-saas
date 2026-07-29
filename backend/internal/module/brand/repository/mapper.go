package repository

import (
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/brand/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

func toModel(b *entity.Brand) *brandModel {
	model := &brandModel{
		BaseEntity:  sharedentity.BaseEntity{ID: b.ID(), Version: b.Version()},
		CompanyID:   b.CompanyID(),
		Code:        b.Code(),
		Name:        b.Name(),
		Description: b.Description(),
		Status:      b.Status().String(),
	}
	if b.IsArchived() {
		model.DeletedAt.Time = *b.DeletedAt()
		model.DeletedAt.Valid = true
	}
	return model
}

func toDomain(model *brandModel) *entity.Brand {
	var deletedAt *time.Time
	if model.DeletedAt.Valid {
		deletedAt = &model.DeletedAt.Time
	}
	b, _ := entity.ReconstituteWithVersion(
		model.ID, model.CompanyID, model.Code, model.Name, model.Description,
		entity.Status(model.Status), model.CreatedAt, model.UpdatedAt, deletedAt, model.Version,
	)
	return b
}

func toDomainSlice(models []brandModel) []*entity.Brand {
	items := make([]*entity.Brand, len(models))
	for i := range models {
		items[i] = toDomain(&models[i])
	}
	return items
}
