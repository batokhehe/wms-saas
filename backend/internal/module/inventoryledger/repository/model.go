package repository

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// ledgerEntryModel is the persistence representation of an InventoryLedgerEntry.
//
// It embeds BaseEntity because the generic base repository requires
// entity.Identifiable for its pagination and scoping. Two of the inherited
// columns are inert here and deliberately so:
//
//   - Version never advances. There is no optimistic locking on a row that is
//     never updated; the column exists only to satisfy the shared contract.
//   - DeletedAt is never set. A ledger entry is never soft-deleted, and the
//     database trigger would reject the UPDATE that GORM's soft delete issues.
//
// The delta columns are STORED even though the aggregate recomputes them on
// load. Persisting them lets the database answer "what moved in this period"
// with a SUM, which is the question a stock report actually asks — recomputing
// per row in Go would mean loading every entry to add up four numbers.
type ledgerEntryModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	PositionID  uuid.UUID `gorm:"type:uuid;not null"`
	ProductID   uuid.UUID `gorm:"type:uuid;not null"`
	WarehouseID uuid.UUID `gorm:"type:uuid;not null"`
	LocationID  uuid.UUID `gorm:"type:uuid;not null"`

	LotNumber    *string `gorm:"column:lot_number;type:citext"`
	SerialNumber *string `gorm:"column:serial_number;type:citext"`

	OwnerID *uuid.UUID `gorm:"column:owner_id;type:uuid"`

	MovementType string `gorm:"column:movement_type;type:varchar(24);not null"`

	ReferenceType  *string    `gorm:"column:reference_type;type:varchar(64)"`
	ReferenceID    *uuid.UUID `gorm:"column:reference_id;type:uuid"`
	DocumentNumber *string    `gorm:"column:document_number;type:varchar(64)"`
	Reason         string     `gorm:"type:text;not null;default:''"`

	ActorID uuid.UUID `gorm:"column:actor_id;type:uuid;not null"`

	BeforeAvailable   int64 `gorm:"column:before_available;not null"`
	BeforeReserved    int64 `gorm:"column:before_reserved;not null"`
	BeforeAllocated   int64 `gorm:"column:before_allocated;not null"`
	BeforeQuarantined int64 `gorm:"column:before_quarantined;not null"`

	AfterAvailable   int64 `gorm:"column:after_available;not null"`
	AfterReserved    int64 `gorm:"column:after_reserved;not null"`
	AfterAllocated   int64 `gorm:"column:after_allocated;not null"`
	AfterQuarantined int64 `gorm:"column:after_quarantined;not null"`

	DeltaAvailable   int64 `gorm:"column:delta_available;not null"`
	DeltaReserved    int64 `gorm:"column:delta_reserved;not null"`
	DeltaAllocated   int64 `gorm:"column:delta_allocated;not null"`
	DeltaQuarantined int64 `gorm:"column:delta_quarantined;not null"`
	DeltaOnHand      int64 `gorm:"column:delta_on_hand;not null"`

	OccurredAt time.Time `gorm:"column:occurred_at;not null;index"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (ledgerEntryModel) TableName() string { return "inventory_ledger_entries" }

// toModel translates an entry into its persistence form, reading through the
// aggregate's getters — the only access anyone has.
func toModel(e *entity.InventoryLedgerEntry) *ledgerEntryModel {
	before, after, delta := e.Before(), e.After(), e.Delta()

	model := &ledgerEntryModel{
		CompanyID:      e.CompanyID(),
		PositionID:     e.PositionID(),
		ProductID:      e.ProductID(),
		WarehouseID:    e.WarehouseID(),
		LocationID:     e.LocationID(),
		LotNumber:      optional(e.LotNumber()),
		SerialNumber:   optional(e.SerialNumber()),
		OwnerID:        e.OwnerID(),
		MovementType:   e.MovementType().String(),
		ReferenceType:  optional(e.ReferenceType()),
		ReferenceID:    e.ReferenceID(),
		DocumentNumber: optional(e.DocumentNumber()),
		Reason:         e.Reason(),
		ActorID:        e.ActorID(),

		BeforeAvailable:   before.Available(),
		BeforeReserved:    before.Reserved(),
		BeforeAllocated:   before.Allocated(),
		BeforeQuarantined: before.Quarantined(),

		AfterAvailable:   after.Available(),
		AfterReserved:    after.Reserved(),
		AfterAllocated:   after.Allocated(),
		AfterQuarantined: after.Quarantined(),

		DeltaAvailable:   delta.Available(),
		DeltaReserved:    delta.Reserved(),
		DeltaAllocated:   delta.Allocated(),
		DeltaQuarantined: delta.Quarantined(),
		DeltaOnHand:      delta.OnHand(),

		OccurredAt: e.OccurredAt(),
	}
	model.ID = e.LedgerID()
	// Version is fixed at 1: the row is written once and never updated.
	model.Version = 1
	return model
}

// toDomain rebuilds an entry from a row.
//
// Value-object construction errors are discarded, matching every other module:
// the data came from the database, which already enforced its constraints, so a
// stored value that fails validation means a migration went wrong — and refusing
// to load the row would make the bad data impossible to inspect.
func toDomain(model *ledgerEntryModel) *entity.InventoryLedgerEntry {
	movementContext, _ := entity.NewMovementContext(
		deref(model.ReferenceType), model.ReferenceID, deref(model.DocumentNumber), model.Reason,
	)
	before, _ := entity.NewBeforeBucket(
		model.BeforeAvailable, model.BeforeReserved, model.BeforeAllocated, model.BeforeQuarantined,
	)
	after, _ := entity.NewAfterBucket(
		model.AfterAvailable, model.AfterReserved, model.AfterAllocated, model.AfterQuarantined,
	)

	entry, _ := entity.Reconstitute(
		model.ID, model.CompanyID,
		model.PositionID, model.ProductID, model.WarehouseID, model.LocationID,
		deref(model.LotNumber), deref(model.SerialNumber),
		model.OwnerID,
		entity.MovementType(model.MovementType),
		movementContext,
		model.ActorID,
		before, after,
		model.OccurredAt,
	)
	return entry
}

// toDomainSlice translates a page of rows.
func toDomainSlice(models []ledgerEntryModel) []*entity.InventoryLedgerEntry {
	result := make([]*entity.InventoryLedgerEntry, 0, len(models))
	for i := range models {
		result = append(result, toDomain(&models[i]))
	}
	return result
}

// optional renders a value as a nullable column: nil when empty, so an absent lot
// or reference is NULL rather than an empty string a query would have to special-case.
func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// deref reads a nullable column back as a plain string.
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
