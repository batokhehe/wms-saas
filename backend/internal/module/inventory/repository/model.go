// Package repository is the inventory module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. The interface it implements is declared in
// repository.go (delivered in the aggregate sprint); this file and
// repository_impl.go are the concrete persistence added in the persistence
// sprint.
//
// It translates between the aggregate — whose fields are unexported and
// unreachable by reflection — and a persistence model GORM can map, exactly as
// the warehouse and product repositories do. Inventory is a single-row
// aggregate (no child collections), so the translation is one model.
package repository

import (
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// inventoryModel is the persistence representation of the Inventory aggregate.
//
// entity.Inventory has unexported fields and no setters — that is what makes it
// an aggregate root, so no caller mutates a quantity except through a behaviour
// that enforces the invariants. GORM maps by reflecting over EXPORTED fields, so
// it cannot touch that type; this model absorbs the ORM instead, and the
// aggregate stays pure. It embeds BaseEntity for id/version/timestamps/soft-
// delete exactly as EntityConvention requires of a persistence type.
//
// lot_number and serial_number are nullable pointers: they are present only for
// the tracking type that requires them, and NULL otherwise. on_hand and reserved
// are the stored counts; available is derived by the aggregate and never stored.
type inventoryModel struct {
	sharedentity.BaseEntity

	CompanyID   uuid.UUID `gorm:"type:uuid;not null;index"`
	WarehouseID uuid.UUID `gorm:"type:uuid;not null"`
	LocationID  uuid.UUID `gorm:"type:uuid;not null"`
	ProductID   uuid.UUID `gorm:"type:uuid;not null"`

	TrackingType string `gorm:"type:varchar(16);not null"`

	LotNumber    *string `gorm:"column:lot_number;type:citext"`
	SerialNumber *string `gorm:"column:serial_number;type:citext"`

	OnHand   int64 `gorm:"column:on_hand;not null;default:0"`
	Reserved int64 `gorm:"column:reserved;not null;default:0"`

	Status string `gorm:"type:varchar(16);not null;default:ACTIVE"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (inventoryModel) TableName() string { return "inventories" }

// toModel translates an aggregate into its persistence form. It reads through
// the aggregate's getters — the only access anyone has — so the persistence
// layer is subject to exactly the same encapsulation as every other caller.
func toModel(inv *entity.Inventory) *inventoryModel {
	model := &inventoryModel{
		CompanyID:    inv.CompanyID(),
		WarehouseID:  inv.WarehouseID(),
		LocationID:   inv.LocationID(),
		ProductID:    inv.ProductID(),
		TrackingType: inv.TrackingType().String(),
		LotNumber:    lotToColumn(inv),
		SerialNumber: serialToColumn(inv),
		OnHand:       inv.OnHand().Value(),
		Reserved:     inv.Reserved().Value(),
		Status:       inv.Status().String(),
		CreatedBy:    inv.CreatedBy(),
		UpdatedBy:    inv.UpdatedBy(),
	}

	model.ID = inv.ID()
	model.Version = inv.Version()
	model.CreatedAt = inv.CreatedAt()
	model.UpdatedAt = inv.UpdatedAt()

	return model
}

// toDomain rebuilds an aggregate from a row.
//
// It calls entity.Reconstitute, NOT entity.NewInventory: loading a row is not a
// business event, and the factory would raise InventoryCreated on every read.
//
// Value-object construction errors are DISCARDED rather than returned, matching
// the warehouse and product modules: the data came from the database, which
// already enforced its constraints, so a stored value that fails validation
// means a migration went wrong — and refusing to load the row would make the bad
// data impossible to inspect or repair. Reconstitute itself still rejects a
// structurally invalid aggregate (reserved above on-hand, a serial without a
// number), which is the check that actually protects an invariant.
func toDomain(model *inventoryModel) *entity.Inventory {
	onHand, _ := entity.NewInventoryQuantity(model.OnHand)
	reserved, _ := entity.NewReservedQuantity(model.Reserved)

	lot := entity.NoLotNumber()
	if model.LotNumber != nil {
		lot, _ = entity.NewLotNumber(*model.LotNumber)
	}
	serial := entity.NoSerialNumber()
	if model.SerialNumber != nil {
		serial, _ = entity.NewSerialNumber(*model.SerialNumber)
	}

	inv, _ := entity.Reconstitute(
		model.ID,
		model.CompanyID,
		model.WarehouseID,
		model.LocationID,
		model.ProductID,
		entity.TrackingType(model.TrackingType),
		entity.InventoryStatus(model.Status),
		onHand,
		reserved,
		lot,
		serial,
		model.Version,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
	)
	return inv
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []inventoryModel) []*entity.Inventory {
	result := make([]*entity.Inventory, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}

// lotToColumn renders the lot number as a nullable column value.
func lotToColumn(inv *entity.Inventory) *string {
	if !inv.HasLot() {
		return nil
	}
	v := inv.Lot().String()
	return &v
}

// serialToColumn renders the serial number as a nullable column value.
func serialToColumn(inv *entity.Inventory) *string {
	if !inv.HasSerial() {
		return nil
	}
	v := inv.Serial().String()
	return &v
}
