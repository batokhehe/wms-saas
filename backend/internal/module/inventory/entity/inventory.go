package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Inventory is the AGGREGATE ROOT for stock state. It represents the current
// state of ONE Product inside ONE Storage Location, and it owns EVERY stock
// transition. Nothing outside this aggregate may mutate a quantity — the fields
// are unexported and there are no setters, so the only way a count changes is by
// calling a behaviour that first enforces the invariants.
//
// # Aggregate invariants (upheld by every behaviour)
//
//	OnHand    >= 0                 (the value objects cannot be negative)
//	Reserved  >= 0
//	OnHand    >= Reserved          (you cannot promise stock you do not hold)
//	Available == OnHand - Reserved (derived, never stored)
//	SERIAL    => OnHand == 1       (a serial is one specific unit, always)
//	LOT       => a lot number is present
//	SERIAL    => a serial number is present
//	NONE      => neither lot nor serial is present
//
// # Cross-aggregate invariants (enforced OUTSIDE the aggregate)
//
// "Warehouse belongs to Company", "Location belongs to Warehouse" and "Product
// belongs to Company" reference OTHER aggregates, which this one cannot load
// without collapsing its consistency boundary. They are enforced at creation
// time by the extension-point providers (see ../port): the aggregate stores the
// verified ids and requires them to be present. "Inventory cannot exist without
// Product" is enforced here — the factory rejects a nil product id.
type Inventory struct {
	id        uuid.UUID
	companyID uuid.UUID

	warehouseID uuid.UUID
	locationID  uuid.UUID
	productID   uuid.UUID

	tracking TrackingType
	status   InventoryStatus

	onHand   InventoryQuantity
	reserved ReservedQuantity

	// lot and serial are zero-valued unless the tracking type requires them.
	lot    LotNumber
	serial SerialNumber

	version uint64

	createdBy uuid.UUID
	updatedBy uuid.UUID
	createdAt time.Time
	updatedAt time.Time

	events []Event
}

// NewInventory is the FACTORY. It is the only way to create a stock position,
// and it always produces one in ACTIVE status with zero reserved and version 1,
// raising InventoryCreated.
//
// initial is the opening on-hand count. For SERIAL it must be exactly one, which
// is the serial invariant established at birth.
func NewInventory(
	id, companyID, warehouseID, locationID, productID uuid.UUID,
	tracking TrackingType,
	lot LotNumber,
	serial SerialNumber,
	initial InventoryQuantity,
	actor uuid.UUID,
	now time.Time,
) (*Inventory, error) {
	if id == uuid.Nil || companyID == uuid.Nil || warehouseID == uuid.Nil ||
		locationID == uuid.Nil || productID == uuid.Nil || actor == uuid.Nil {
		return nil, apperror.Validation("inventory, company, warehouse, location, product and actor ids are required")
	}
	if !tracking.Valid() {
		return nil, apperror.Validation("invalid tracking type")
	}
	if err := validateTracking(tracking, lot, serial); err != nil {
		return nil, err
	}
	if tracking == TrackingSerial && initial.Value() != 1 {
		return nil, apperror.Validation("serial inventory must be created with a quantity of exactly one")
	}

	reserved, _ := NewReservedQuantity(0)

	i := &Inventory{
		id:          id,
		companyID:   companyID,
		warehouseID: warehouseID,
		locationID:  locationID,
		productID:   productID,
		tracking:    tracking,
		status:      StatusActive,
		onHand:      initial,
		reserved:    reserved,
		lot:         lot,
		serial:      serial,
		version:     1,
		createdBy:   actor,
		updatedBy:   actor,
		createdAt:   now,
		updatedAt:   now,
	}

	i.record(EventInventoryCreated, actor, now, map[string]any{
		"product_id":   productID.String(),
		"location_id":  locationID.String(),
		"warehouse_id": warehouseID.String(),
		"tracking":     tracking.String(),
		"status":       i.status.String(),
		"on_hand":      i.onHand.Value(),
		"lot":          lot.String(),
		"serial":       serial.String(),
	})

	return i, nil
}

// Reconstitute restores stored state WITHOUT raising events. Loading a row is
// not a business event: the factory would raise InventoryCreated on every read.
// It re-validates the persisted state so a corrupt row is refused rather than
// silently trusted.
func Reconstitute(
	id, companyID, warehouseID, locationID, productID uuid.UUID,
	tracking TrackingType,
	status InventoryStatus,
	onHand InventoryQuantity,
	reserved ReservedQuantity,
	lot LotNumber,
	serial SerialNumber,
	version uint64,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*Inventory, error) {
	if version == 0 || id == uuid.Nil || companyID == uuid.Nil || warehouseID == uuid.Nil ||
		locationID == uuid.Nil || productID == uuid.Nil {
		return nil, apperror.Validation("invalid persisted inventory state")
	}
	if !tracking.Valid() || !status.Valid() {
		return nil, apperror.Validation("invalid persisted inventory state")
	}
	if err := validateTracking(tracking, lot, serial); err != nil {
		return nil, err
	}
	if !onHand.coversReserved(reserved) {
		return nil, apperror.Validation("persisted on-hand is below reserved")
	}
	if tracking == TrackingSerial && onHand.Value() != 1 {
		return nil, apperror.Validation("persisted serial inventory must have a quantity of exactly one")
	}

	return &Inventory{
		id:          id,
		companyID:   companyID,
		warehouseID: warehouseID,
		locationID:  locationID,
		productID:   productID,
		tracking:    tracking,
		status:      status,
		onHand:      onHand,
		reserved:    reserved,
		lot:         lot,
		serial:      serial,
		version:     version,
		createdBy:   createdBy,
		updatedBy:   updatedBy,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}, nil
}

// validateTracking enforces the lot/serial presence rules for a tracking type.
func validateTracking(tracking TrackingType, lot LotNumber, serial SerialNumber) error {
	switch tracking {
	case TrackingNone:
		if !lot.IsZero() || !serial.IsZero() {
			return apperror.Validation("untracked inventory must not carry a lot or serial number")
		}
	case TrackingLot:
		if lot.IsZero() {
			return apperror.Validation("lot-tracked inventory requires a lot number")
		}
		if !serial.IsZero() {
			return apperror.Validation("lot-tracked inventory must not carry a serial number")
		}
	case TrackingSerial:
		if serial.IsZero() {
			return apperror.Validation("serial-tracked inventory requires a serial number")
		}
		if !lot.IsZero() {
			return apperror.Validation("serial-tracked inventory must not carry a lot number")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Behaviours
// ---------------------------------------------------------------------------
//
// Every behaviour follows the same discipline: refuse if the record is not in a
// state that permits it, compute the new counts through the value objects (which
// reject negativity and overflow), verify the aggregate invariant, mutate, touch
// the audit fields, and record exactly one event. No behaviour performs a
// partial mutation before it can fail.

// Increase adds received or found stock to the on-hand count.
func (i *Inventory) Increase(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialQuantityChange(); err != nil {
		return err
	}
	next, err := i.onHand.add(amount)
	if err != nil {
		return err
	}
	i.onHand = next
	i.touch(actor, now)
	i.record(EventInventoryIncreased, actor, now, i.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// Decrease removes stock from the on-hand count — a consumption, damage or
// write-off. It may not drop on-hand below what is reserved.
func (i *Inventory) Decrease(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialQuantityChange(); err != nil {
		return err
	}
	next, err := i.onHand.sub(amount)
	if err != nil {
		return err
	}
	if !next.coversReserved(i.reserved) {
		return apperror.Conflict("cannot decrease on-hand below the reserved quantity")
	}
	i.onHand = next
	i.touch(actor, now)
	i.record(EventInventoryDecreased, actor, now, i.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// Reserve promises available stock to an order. It may reserve only what is
// available (on-hand minus already-reserved).
func (i *Inventory) Reserve(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if !i.Available().covers(amount) {
		return apperror.Conflict("insufficient available quantity to reserve")
	}
	next, err := i.reserved.add(amount)
	if err != nil {
		return err
	}
	i.reserved = next
	i.touch(actor, now)
	i.record(EventInventoryReserved, actor, now, i.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// ReleaseReservation returns promised stock to the available pool — an order was
// cancelled or short-picked. It cannot release more than is reserved.
func (i *Inventory) ReleaseReservation(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	next, err := i.reserved.sub(amount)
	if err != nil {
		return err
	}
	i.reserved = next
	i.touch(actor, now)
	i.record(EventInventoryReleased, actor, now, i.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// Adjust sets the on-hand count to an absolute corrected value — a manual
// correction outside a formal cycle count. The new value may not fall below the
// reserved quantity.
func (i *Inventory) Adjust(counted InventoryQuantity, reason string, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialAbsoluteChange(counted); err != nil {
		return err
	}
	if !counted.coversReserved(i.reserved) {
		return apperror.Conflict("adjusted on-hand cannot be below the reserved quantity")
	}
	previous := i.onHand
	i.onHand = counted
	i.touch(actor, now)
	i.record(EventInventoryAdjusted, actor, now, i.withState(map[string]any{
		"previous_on_hand": previous.Value(),
		"reason":           reason,
	}))
	return nil
}

// TransferOut removes available stock that is moving to another location. Only
// available (unreserved) stock may move; the paired TransferIn lands on the
// destination's own inventory record.
func (i *Inventory) TransferOut(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialQuantityChange(); err != nil {
		return err
	}
	if !i.Available().covers(amount) {
		return apperror.Conflict("insufficient available quantity to transfer out")
	}
	next, err := i.onHand.sub(amount)
	if err != nil {
		return err
	}
	i.onHand = next
	i.touch(actor, now)
	i.record(EventInventoryTransferred, actor, now, i.withState(map[string]any{
		"amount":    amount.Value(),
		"direction": TransferDirectionOut,
	}))
	return nil
}

// TransferIn adds stock arriving from another location.
func (i *Inventory) TransferIn(amount Quantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialQuantityChange(); err != nil {
		return err
	}
	next, err := i.onHand.add(amount)
	if err != nil {
		return err
	}
	i.onHand = next
	i.touch(actor, now)
	i.record(EventInventoryTransferred, actor, now, i.withState(map[string]any{
		"amount":    amount.Value(),
		"direction": TransferDirectionIn,
	}))
	return nil
}

// CompleteCycleCount reconciles on-hand to a physically counted value and
// records the variance. The counted value may not fall below the reserved
// quantity — reserved stock the count cannot find is a discrepancy that a
// simple reconciliation must not silently absorb.
func (i *Inventory) CompleteCycleCount(counted InventoryQuantity, actor uuid.UUID, now time.Time) error {
	if err := i.requireActive(); err != nil {
		return err
	}
	if err := i.rejectSerialAbsoluteChange(counted); err != nil {
		return err
	}
	if !counted.coversReserved(i.reserved) {
		return apperror.Conflict("counted quantity cannot be below the reserved quantity")
	}
	previous := i.onHand
	variance := counted.Value() - previous.Value()
	i.onHand = counted
	i.touch(actor, now)
	i.record(EventCycleCountCompleted, actor, now, i.withState(map[string]any{
		"previous_on_hand": previous.Value(),
		"counted":          counted.Value(),
		"variance":         variance,
	}))
	return nil
}

// Lock freezes the record. No quantity may move while it is LOCKED.
func (i *Inventory) Lock(actor uuid.UUID, now time.Time) error {
	if i.status == StatusLocked {
		return apperror.Conflict("inventory is already locked")
	}
	i.status = StatusLocked
	i.touch(actor, now)
	i.record(EventInventoryLocked, actor, now, map[string]any{"status": i.status.String()})
	return nil
}

// Unlock returns the record to ACTIVE.
func (i *Inventory) Unlock(actor uuid.UUID, now time.Time) error {
	if i.status == StatusActive {
		return apperror.Conflict("inventory is not locked")
	}
	i.status = StatusActive
	i.touch(actor, now)
	i.record(EventInventoryUnlocked, actor, now, map[string]any{"status": i.status.String()})
	return nil
}

// requireActive refuses a movement while the record is LOCKED. Lock/Unlock are
// the only behaviours that do not call it.
func (i *Inventory) requireActive() error {
	if i.status != StatusActive {
		return apperror.Conflict("inventory is locked and cannot be modified")
	}
	return nil
}

// rejectSerialQuantityChange refuses a relative on-hand change on a SERIAL
// record. A serial is one specific unit; its on-hand is fixed at one for the
// life of the record, so Increase/Decrease/Transfer are meaningless on it.
// Reserve/Release/Lock/Unlock remain available.
func (i *Inventory) rejectSerialQuantityChange() error {
	if i.tracking == TrackingSerial {
		return apperror.Conflict("serial inventory quantity is fixed at one and cannot change")
	}
	return nil
}

// rejectSerialAbsoluteChange refuses an absolute on-hand set on a SERIAL record
// unless it confirms the fixed quantity of one. A cycle count that confirms the
// serial is present is permitted (a no-op); any other counted value is not.
func (i *Inventory) rejectSerialAbsoluteChange(counted InventoryQuantity) error {
	if i.tracking == TrackingSerial && counted.Value() != 1 {
		return apperror.Conflict("serial inventory quantity is fixed at one")
	}
	return nil
}

// touch records who last changed the record and when. Version is NOT advanced
// here: it is owned by the persistence layer, and the aggregate exposes it
// read-only.
func (i *Inventory) touch(actor uuid.UUID, now time.Time) {
	i.updatedBy = actor
	i.updatedAt = now
}

// ---------------------------------------------------------------------------
// Accessors (read-only)
// ---------------------------------------------------------------------------

// ID returns the inventory identity.
func (i *Inventory) ID() uuid.UUID { return i.id }

// CompanyID returns the owning tenant.
func (i *Inventory) CompanyID() uuid.UUID { return i.companyID }

// WarehouseID returns the warehouse the stock sits in.
func (i *Inventory) WarehouseID() uuid.UUID { return i.warehouseID }

// LocationID returns the storage location the stock sits in.
func (i *Inventory) LocationID() uuid.UUID { return i.locationID }

// ProductID returns the product the stock is of.
func (i *Inventory) ProductID() uuid.UUID { return i.productID }

// TrackingType returns how the stock is individuated.
func (i *Inventory) TrackingType() TrackingType { return i.tracking }

// Status returns the availability state.
func (i *Inventory) Status() InventoryStatus { return i.status }

// OnHand returns the physical count.
func (i *Inventory) OnHand() InventoryQuantity { return i.onHand }

// Reserved returns the promised count.
func (i *Inventory) Reserved() ReservedQuantity { return i.reserved }

// Available returns on-hand minus reserved — always non-negative.
func (i *Inventory) Available() AvailableQuantity { return available(i.onHand, i.reserved) }

// Lot returns the lot number, or its zero value when not lot-tracked.
func (i *Inventory) Lot() LotNumber { return i.lot }

// Serial returns the serial number, or its zero value when not serial-tracked.
func (i *Inventory) Serial() SerialNumber { return i.serial }

// HasLot reports whether a lot number is set.
func (i *Inventory) HasLot() bool { return !i.lot.IsZero() }

// HasSerial reports whether a serial number is set.
func (i *Inventory) HasSerial() bool { return !i.serial.IsZero() }

// IsLocked reports whether the record is frozen.
func (i *Inventory) IsLocked() bool { return i.status == StatusLocked }

// Version returns the optimistic-lock token. It is read-only in the domain;
// repositories are the sole owner of its advancement.
func (i *Inventory) Version() uint64 { return i.version }

// CreatedBy returns the actor who created the record.
func (i *Inventory) CreatedBy() uuid.UUID { return i.createdBy }

// UpdatedBy returns the actor who last changed the record.
func (i *Inventory) UpdatedBy() uuid.UUID { return i.updatedBy }

// CreatedAt returns when the record was created.
func (i *Inventory) CreatedAt() time.Time { return i.createdAt }

// UpdatedAt returns when the record was last changed.
func (i *Inventory) UpdatedAt() time.Time { return i.updatedAt }
