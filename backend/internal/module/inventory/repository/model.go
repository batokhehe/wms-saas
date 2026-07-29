package repository

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// positionModel is the persistence representation of the InventoryPosition
// aggregate.
//
// entity.InventoryPosition has unexported fields GORM cannot reflect over, and
// exporting them would delete the encapsulation that makes the bucket model
// trustworthy. So this model absorbs the ORM and the aggregate stays pure.
//
// The four buckets are stored; ON-HAND IS NOT. It is derived by the aggregate,
// and persisting it would create a second source of truth that could disagree
// with its own parts — exactly the drift the bucket model exists to prevent.
type positionModel struct {
	sharedentity.BaseEntity

	CompanyID   uuid.UUID `gorm:"type:uuid;not null;index"`
	WarehouseID uuid.UUID `gorm:"type:uuid;not null"`
	LocationID  uuid.UUID `gorm:"type:uuid;not null"`
	ProductID   uuid.UUID `gorm:"type:uuid;not null"`

	TrackingType string  `gorm:"column:tracking_type;type:varchar(16);not null"`
	LotNumber    *string `gorm:"column:lot_number;type:citext"`
	SerialNumber *string `gorm:"column:serial_number;type:citext"`

	Available   int64 `gorm:"not null;default:0"`
	Reserved    int64 `gorm:"not null;default:0"`
	Allocated   int64 `gorm:"not null;default:0"`
	Quarantined int64 `gorm:"not null;default:0"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (positionModel) TableName() string { return "inventory_positions" }

// toModel translates an aggregate into its persistence form, reading through the
// aggregate's getters — the only access anyone has.
func toModel(p *entity.InventoryPosition) *positionModel {
	attrs := p.Attributes()

	model := &positionModel{
		CompanyID:    p.CompanyID(),
		WarehouseID:  p.WarehouseID(),
		LocationID:   p.LocationID(),
		ProductID:    p.ProductID(),
		TrackingType: attrs.Tracking().String(),
		LotNumber:    optional(attrs.Lot().String()),
		SerialNumber: optional(attrs.Serial().String()),
		Available:    p.Available().Value(),
		Reserved:     p.Reserved().Value(),
		Allocated:    p.Allocated().Value(),
		Quarantined:  p.Quarantined().Value(),
		CreatedBy:    p.CreatedBy(),
		UpdatedBy:    p.UpdatedBy(),
	}
	model.ID = p.ID()
	model.Version = p.Version()
	model.CreatedAt = p.CreatedAt()
	model.UpdatedAt = p.UpdatedAt()
	return model
}

// toDomain rebuilds an aggregate from a row via entity.Reconstitute, NOT the
// factory: loading a row is not a business event.
//
// Value-object construction errors are discarded, matching every other module:
// the data came from the database, which already enforced its constraints, so a
// stored value that fails validation means a migration went wrong — and refusing
// to load the row would make the bad data impossible to inspect or repair.
// Reconstitute still rejects a structurally invalid aggregate.
func toDomain(model *positionModel) *entity.InventoryPosition {
	lot := entity.NoLotNumber()
	if model.LotNumber != nil {
		if built, err := entity.NewLotNumber(*model.LotNumber); err == nil {
			lot = built
		}
	}
	serial := entity.NoSerialNumber()
	if model.SerialNumber != nil {
		if built, err := entity.NewSerialNumber(*model.SerialNumber); err == nil {
			serial = built
		}
	}

	attrs, _ := entity.NewStockAttributes(entity.TrackingType(model.TrackingType), lot, serial)
	key, _ := entity.NewStockKey(model.CompanyID, model.WarehouseID, model.LocationID, model.ProductID, attrs)

	position, _ := entity.Reconstitute(
		model.ID,
		key,
		bucket(model.Available),
		bucket(model.Reserved),
		bucket(model.Allocated),
		bucket(model.Quarantined),
		model.Version,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
	)
	return position
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []positionModel) []*entity.InventoryPosition {
	result := make([]*entity.InventoryPosition, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}

// bucket rebuilds a balance, falling back to empty for a stored negative that
// the CHECK constraint should already have prevented.
func bucket(v int64) entity.QuantityBucket {
	built, err := entity.NewQuantityBucket(v)
	if err != nil {
		return entity.EmptyBucket()
	}
	return built
}

// optional renders a value-object string as a nullable column value, so an unset
// lot or serial is NULL rather than an empty string — which matters because the
// unique indexes treat NULLs as distinct.
func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
