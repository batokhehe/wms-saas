package repository

import "github.com/batokhehe/wms-saas/backend/internal/module/uom/entity"

// toModel isolates aggregate-to-row translation. The aggregate has no exported
// fields and never receives persistence-model concerns.
func toModel(uom *entity.UOM) *uomModel {
	model := &uomModel{
		Code:        uom.Code(),
		Name:        uom.Name(),
		Description: uom.Description(),
		Status:      string(uom.Status()),
	}

	model.ID = uom.ID()
	model.Version = uom.Version()
	model.CreatedAt = uom.CreatedAt()
	model.UpdatedAt = uom.UpdatedAt()

	return model
}

// toDomain rebuilds an aggregate without raising creation events. Persistence
// rows are expected to satisfy the migration-backed aggregate invariants.
func toDomain(model *uomModel) *entity.UOM {
	uom, _ := entity.ReconstituteWithVersion(
		model.ID,
		model.Code,
		model.Name,
		model.Description,
		entity.Status(model.Status),
		model.CreatedAt,
		model.UpdatedAt,
		model.Version,
	)
	return uom
}

func toDomainSlice(models []uomModel) []*entity.UOM {
	items := make([]*entity.UOM, 0, len(models))
	for i := range models {
		items = append(items, toDomain(&models[i]))
	}
	return items
}
