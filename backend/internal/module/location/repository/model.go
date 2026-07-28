// Package repository is the location module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// Like the warehouse module, it carries the extra responsibility of translating
// between an aggregate whose fields are unexported and a persistence model GORM
// can map. See model.go's doc comment.
package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// locationModel is the persistence representation of the StorageLocation
// aggregate.
//
// # Why a separate type exists
//
// entity.StorageLocation has unexported fields and no setters, which is what
// makes it an aggregate root — no caller can reach ACTIVE except through
// Unlock(). GORM maps by reflecting over EXPORTED fields, so it cannot read or
// write that type at all.
//
// This model absorbs the ORM. It embeds shared/entity.BaseEntity and carries
// GORM tags exactly as EntityConvention requires — that convention governs
// PERSISTENCE types, and this is one. The aggregate is not.
//
// The cost is one translation per read and per write, written once here and
// covered by a round-trip integration test, because a hand-written translation
// is exactly where a forgotten field causes silent data loss.
type locationModel struct {
	sharedentity.BaseEntity

	CompanyID   uuid.UUID `gorm:"type:uuid;not null;index"`
	WarehouseID uuid.UUID `gorm:"type:uuid;not null;index"`

	Code string `gorm:"type:citext;not null"`

	Zone  string `gorm:"type:varchar(32);not null"`
	Aisle string `gorm:"type:varchar(32);not null;default:''"`
	Rack  string `gorm:"type:varchar(32);not null;default:''"`
	Level string `gorm:"type:varchar(32);not null;default:''"`
	Bin   string `gorm:"type:varchar(32);not null;default:''"`

	// A pointer so an absent barcode is written as SQL NULL rather than the
	// empty string. The unique index is partial on `barcode IS NOT NULL`, and
	// empty strings would collide with each other where NULLs do not.
	Barcode *string `gorm:"type:varchar(64)"`

	Status string `gorm:"type:varchar(16);not null;default:ACTIVE"`

	PickingPriority int  `gorm:"not null;default:100"`
	AllowMixedSKU   bool `gorm:"not null;default:false"`
	AllowOverflow   bool `gorm:"not null;default:false"`

	// Capacity limits are stored as decimal STRINGS on the Go side and
	// NUMERIC(14,3) in the column.
	//
	// Not float64: a WMS adds and subtracts these thousands of times a day, and
	// binary floating point accumulates error on every operation — the
	// discrepancy surfaces as a capacity check that passes when it should fail.
	// The aggregate holds them as *big.Rat; strings are the lossless transport
	// between that and the driver.
	MaxWeight *string `gorm:"type:numeric(14,3)"`
	MaxVolume *string `gorm:"type:numeric(14,3)"`
	MaxPallet *int    `gorm:"type:integer"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (locationModel) TableName() string { return "storage_locations" }

// toModel translates an aggregate into its persistence form.
//
// It reads through the aggregate's getters, which is the only access anyone
// has — the persistence layer is subject to exactly the same encapsulation as
// every other caller.
func toModel(l *entity.StorageLocation) *locationModel {
	coordinate := l.Coordinate()
	capacity := l.Capacity()

	model := &locationModel{
		CompanyID:       l.CompanyID(),
		WarehouseID:     l.WarehouseID(),
		Code:            l.Code().String(),
		Zone:            coordinate.Zone(),
		Aisle:           coordinate.Aisle(),
		Rack:            coordinate.Rack(),
		Level:           coordinate.Level(),
		Bin:             coordinate.Bin(),
		Barcode:         nullableString(l.Barcode().String()),
		Status:          l.Status().String(),
		PickingPriority: l.PickingPriority(),
		AllowMixedSKU:   l.AllowMixedSKU(),
		AllowOverflow:   l.AllowOverflow(),
		MaxWeight:       nullableString(capacity.MaxWeight().String()),
		MaxVolume:       nullableString(capacity.MaxVolume().String()),
		MaxPallet:       capacity.MaxPallet(),
		CreatedBy:       l.CreatedBy(),
		UpdatedBy:       l.UpdatedBy(),
	}

	model.ID = l.ID()
	model.Version = l.Version()
	model.CreatedAt = l.CreatedAt()
	model.UpdatedAt = l.UpdatedAt()

	// gorm.DeletedAt rather than a *time.Time, so GORM's automatic soft-delete
	// filtering applies to every query against this model.
	if at := l.DeletedAt(); at != nil {
		model.DeletedAt.Time = *at
		model.DeletedAt.Valid = true
	}

	return model
}

// toDomain rebuilds an aggregate from a row.
//
// It calls entity.Reconstitute, NOT the factory. Loading a row is not a
// business event: constructing through NewStorageLocation would raise
// LocationCreated on every read, and a warehouse listing 500 locations would
// publish 500 creations.
//
// Value-object construction errors are DISCARDED. The data came from the
// database, which already enforced the constraints; a stored code that fails
// validation means a migration went wrong, and refusing to load the row would
// make the bad data impossible to inspect or repair. The zero value is
// preserved instead, which is visible and fixable.
func toDomain(model *locationModel) *entity.StorageLocation {
	code, _ := entity.NewLocationCode(model.Code)
	coordinate, _ := entity.NewCoordinate(
		model.Zone, model.Aisle, model.Rack, model.Level, model.Bin)
	barcode, _ := entity.NewBarcode(derefString(model.Barcode))

	maxWeight, _ := entity.NewQuantity(derefString(model.MaxWeight), "max_weight")
	maxVolume, _ := entity.NewQuantity(derefString(model.MaxVolume), "max_volume")
	capacity, _ := entity.NewCapacity(maxWeight, maxVolume, model.MaxPallet)

	var deletedAt *time.Time
	if model.DeletedAt.Valid {
		at := model.DeletedAt.Time
		deletedAt = &at
	}

	return entity.ReconstituteWithVersion(
		model.ID,
		model.CompanyID,
		model.WarehouseID,
		model.Version,
		code,
		coordinate,
		barcode,
		entity.Status(model.Status),
		model.PickingPriority,
		model.AllowMixedSKU,
		model.AllowOverflow,
		capacity,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
		deletedAt,
	)
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []locationModel) []*entity.StorageLocation {
	result := make([]*entity.StorageLocation, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}

// nullableString maps "" to SQL NULL.
//
// The distinction matters for both the barcode (whose unique index is partial
// on NOT NULL) and the capacity limits (where NULL means "not measured" and is
// genuinely different from zero).
func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// derefString maps SQL NULL back to "".
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ensure gorm is referenced by the model's soft-delete field type.
var _ = gorm.DeletedAt{}
