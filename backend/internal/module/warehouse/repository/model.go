// Package repository is the warehouse module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// It carries an extra responsibility the other modules do not have: translating
// between the aggregate — whose fields are unexported and unreachable by
// reflection — and a persistence model GORM can map. See model.go's doc comment.
package repository

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// warehouseModel is the persistence representation of the Warehouse aggregate.
//
// # Why a separate type exists
//
// entity.Warehouse has unexported fields and no setters, which is what makes it
// an aggregate root — no caller can reach ACTIVE except through Activate().
// GORM maps by reflecting over EXPORTED fields, so it cannot read or write that
// type at all.
//
// The two common escapes are both worse:
//
//   - Export the aggregate's fields. This deletes the encapsulation the sprint
//     exists to establish; every invariant becomes advisory.
//   - Implement database/sql interfaces on the aggregate. That drags persistence
//     concerns into the innermost layer and still requires exported fields for
//     GORM's column mapping.
//
// So the aggregate stays pure and this model absorbs the ORM. It embeds
// shared/entity.BaseEntity and carries GORM tags exactly as EntityConvention
// requires — that convention governs PERSISTENCE types, and this is one. The
// aggregate is not.
//
// The cost is one translation per read and per write, written once in this file.
type warehouseModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	Code        string `gorm:"type:citext;not null"`
	Name        string `gorm:"type:citext;not null"`
	Description string `gorm:"type:text;not null;default:''"`
	Type        string `gorm:"type:varchar(16);not null"`
	Status      string `gorm:"type:varchar(16);not null;default:DRAFT"`

	Address      string `gorm:"type:text;not null;default:''"`
	ContactName  string `gorm:"type:varchar(255);not null;default:''"`
	ContactPhone string `gorm:"type:varchar(32);not null;default:''"`

	DefaultReceivingZoneID *uuid.UUID `gorm:"type:uuid"`
	DefaultShippingZoneID  *uuid.UUID `gorm:"type:uuid"`
	DefaultStagingZoneID   *uuid.UUID `gorm:"type:uuid"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (warehouseModel) TableName() string { return "warehouses" }

// toModel translates an aggregate into its persistence form.
//
// It reads through the aggregate's getters, which is the only access anyone
// has. That is not a limitation to work around — it means the persistence layer
// is subject to exactly the same encapsulation as every other caller.
func toModel(w *entity.Warehouse) *warehouseModel {
	model := &warehouseModel{
		CompanyID:              w.CompanyID(),
		Code:                   w.Code().String(),
		Name:                   w.Name(),
		Description:            w.Description(),
		Type:                   w.Type().String(),
		Status:                 w.Status().String(),
		Address:                w.Address().String(),
		ContactName:            w.Contact().Name(),
		ContactPhone:           w.Contact().Phone(),
		DefaultReceivingZoneID: w.Zones().Receiving(),
		DefaultShippingZoneID:  w.Zones().Shipping(),
		DefaultStagingZoneID:   w.Zones().Staging(),
		CreatedBy:              w.CreatedBy(),
		UpdatedBy:              w.UpdatedBy(),
	}

	model.ID = w.ID()
	model.Version = w.Version()
	model.CreatedAt = w.CreatedAt()
	model.UpdatedAt = w.UpdatedAt()

	// gorm.DeletedAt rather than a *time.Time, so GORM's automatic
	// soft-delete filtering applies to every query against this model.
	if at := w.DeletedAt(); at != nil {
		model.DeletedAt.Time = *at
		model.DeletedAt.Valid = true
	}

	return model
}

// toDomain rebuilds an aggregate from a row.
//
// It calls entity.Reconstitute, NOT entity.NewWarehouse. Loading a row is not a
// business event: constructing through the factory would raise
// WarehouseCreated on every read, and an audit log would claim the warehouse
// was created once per page view.
//
// Value-object construction errors are DISCARDED rather than returned. The data
// came from the database, which already enforced the constraints; a stored code
// that fails validation means a migration went wrong, and refusing to load the
// row would make the bad data impossible to inspect or repair. The zero value
// is preserved instead, which is visible and fixable.
func toDomain(model *warehouseModel) *entity.Warehouse {
	code, _ := entity.NewCode(model.Code)
	address, _ := entity.NewAddress(model.Address)
	contact, _ := entity.NewContact(model.ContactName, model.ContactPhone)

	zones := entity.NewZones(
		model.DefaultReceivingZoneID,
		model.DefaultShippingZoneID,
		model.DefaultStagingZoneID,
	)

	var deletedAt *time.Time
	if model.DeletedAt.Valid {
		at := model.DeletedAt.Time
		deletedAt = &at
	}

	return entity.ReconstituteWithVersion(
		model.ID,
		model.CompanyID,
		model.Version,
		code,
		model.Name,
		model.Description,
		entity.Type(model.Type),
		entity.Status(model.Status),
		address,
		contact,
		zones,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
		deletedAt,
	)
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []warehouseModel) []*entity.Warehouse {
	result := make([]*entity.Warehouse, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}
