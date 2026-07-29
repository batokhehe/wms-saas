package repository

import (
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/module/category/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

func toModel(c *entity.Category) *categoryModel {
	model := &categoryModel{
		BaseEntity:  sharedentity.BaseEntity{ID: c.ID(), Version: c.Version()},
		CompanyID:   c.CompanyID(),
		Code:        c.Code(),
		Name:        c.Name(),
		Description: c.Description(),
		Status:      c.Status().String(),
	}
	if c.IsArchived() {
		model.DeletedAt.Time = *c.DeletedAt() // Need to add DeletedAt() getter to entity
		model.DeletedAt.Valid = true
	}
	return model
}

func toDomain(model *categoryModel) *entity.Category {
	var deletedAt *time.Time
	if model.DeletedAt.Valid {
		deletedAt = &model.DeletedAt.Time
	}
	c, _ := entity.ReconstituteWithVersion(
		model.ID, model.CompanyID, model.Code, model.Name, model.Description,
		entity.Status(model.Status), model.CreatedAt, model.UpdatedAt, deletedAt, model.Version,
	)
	return c
}

func toDomainSlice(models []categoryModel) []*entity.Category {
	items := make([]*entity.Category, len(models))
	for i := range models {
		items[i] = toDomain(&models[i])
	}
	return items
}
