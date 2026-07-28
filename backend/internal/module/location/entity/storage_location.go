package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// StorageLocation is the aggregate root of the location domain.
//
// # Why it is a separate aggregate from Warehouse
//
// A location belongs to exactly one warehouse, which is the classic shape
// people model as a child collection. That would be wrong here.
//
// A large distribution centre has tens of thousands of locations. Loading a
// warehouse "with its locations" would be unusable, and — worse — making them
// one aggregate would mean locking the entire warehouse to change one bin's
// capacity, serialising every operation on the site behind a relabelled shelf.
//
// So they are two aggregates, and the reference is BY ID in one direction only.
// StorageLocation holds a WarehouseID; it never holds a *Warehouse. Whether
// that warehouse exists and is usable is a question the service asks through
// service.WarehouseVerifier, because an aggregate that loaded another aggregate
// would collapse the consistency boundary that makes each independent.
//
// # Why every field is unexported
//
// Same reason as the Warehouse aggregate. "A LOCKED location never accepts
// stock" is only a guarantee if there is no way to reach ACTIVE except through
// Unlock(). Exported fields make it advisory.
//
// GORM cannot map this type, and that cost is paid in repository/model.go.
type StorageLocation struct {
	id          uuid.UUID
	companyID   uuid.UUID
	warehouseID uuid.UUID
	version     uint64

	code       LocationCode
	coordinate Coordinate
	barcode    Barcode
	status     Status

	pickingPriority int
	allowMixedSKU   bool
	allowOverflow   bool
	capacity        Capacity

	createdBy uuid.UUID
	updatedBy uuid.UUID

	createdAt time.Time
	updatedAt time.Time
	deletedAt *time.Time

	// events accumulates what happened during the current unit of work. The
	// aggregate records; the service publishes.
	events []Event
}

// DefaultPickingPriority sorts a location mid-pack when nothing is specified.
//
// A middle value rather than zero, so a later "put this one first" and a later
// "put this one last" are both expressible without renumbering everything.
const DefaultPickingPriority = 100

// ---------- Construction ----------

// NewStorageLocation creates a location in ACTIVE.
//
// # Why ACTIVE and not a draft
//
// This differs from Warehouse, which starts in DRAFT, and the difference is
// deliberate. A warehouse must be commissioned — it needs an address, a contact
// and zones before anyone can ship from it. A location has no such prerequisite:
// once its coordinate exists, the physical shelf exists, and a rack that is
// built is a rack that can hold stock.
//
// Forcing a DRAFT step would mean importing a 20,000-location rack layout and
// then activating all 20,000 of them, which is ceremony with no invariant behind
// it. A location that is not yet usable is expressed with INACTIVE, and callers
// that want that say so.
//
// The code defaults to the coordinate — see DeriveCode — unless one is supplied.
func NewStorageLocation(
	id uuid.UUID,
	companyID, warehouseID uuid.UUID,
	coordinate Coordinate,
	explicitCode LocationCode,
	actorID uuid.UUID,
	now time.Time,
) (*StorageLocation, error) {
	if coordinate.IsZero() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "zone", Rule: "required", Message: "a coordinate is required",
		}).WithOp("location.NewStorageLocation")
	}

	code := explicitCode
	if code.IsZero() {
		derived, err := DeriveCode(coordinate)
		if err != nil {
			return nil, err
		}
		code = derived
	}

	l := &StorageLocation{
		id:              id,
		companyID:       companyID,
		warehouseID:     warehouseID,
		version:         1,
		code:            code,
		coordinate:      coordinate,
		status:          StatusActive,
		pickingPriority: DefaultPickingPriority,
		capacity:        UnlimitedCapacity(),
		createdBy:       actorID,
		updatedBy:       actorID,
		createdAt:       now,
		updatedAt:       now,
	}

	l.record(newEvent(EventLocationCreated, l, actorID, now).
		With("code", code.String()).
		With("coordinate", coordinate.String()))

	return l, nil
}

// Reconstitute rebuilds a location from storage WITHOUT raising events.
//
// The distinction from the factory is fundamental: loading a row is not a
// business event. If reconstitution raised LocationCreated, an audit log would
// claim every location was created once per page view — and a warehouse listing
// 500 locations would publish 500 creations.
//
// It performs no validation. The data came from the database, which already
// enforced the constraints; validating here would mean a row that became
// invalid through a migration could never be loaded, not even to be repaired.
func Reconstitute(
	id, companyID, warehouseID uuid.UUID,
	code LocationCode,
	coordinate Coordinate,
	barcode Barcode,
	status Status,
	pickingPriority int,
	allowMixedSKU, allowOverflow bool,
	capacity Capacity,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
	deletedAt *time.Time,
) *StorageLocation {
	return ReconstituteWithVersion(id, companyID, warehouseID, 1, code, coordinate, barcode,
		status, pickingPriority, allowMixedSKU, allowOverflow, capacity, createdBy, updatedBy,
		createdAt, updatedAt, deletedAt)
}

// ReconstituteWithVersion restores the repository-owned concurrency token.
// Aggregate methods intentionally expose no way to mutate it.
func ReconstituteWithVersion(
	id, companyID, warehouseID uuid.UUID,
	version uint64,
	code LocationCode,
	coordinate Coordinate,
	barcode Barcode,
	status Status,
	pickingPriority int,
	allowMixedSKU, allowOverflow bool,
	capacity Capacity,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
	deletedAt *time.Time,
) *StorageLocation {
	return &StorageLocation{
		id:              id,
		companyID:       companyID,
		warehouseID:     warehouseID,
		version:         version,
		code:            code,
		coordinate:      coordinate,
		barcode:         barcode,
		status:          status,
		pickingPriority: pickingPriority,
		allowMixedSKU:   allowMixedSKU,
		allowOverflow:   allowOverflow,
		capacity:        capacity,
		createdBy:       createdBy,
		updatedBy:       updatedBy,
		createdAt:       createdAt,
		updatedAt:       updatedAt,
		deletedAt:       deletedAt,
	}
}

// ---------- Getters ----------
//
// Read access only. Value objects are returned by value, so a caller holding
// one cannot reach back into the aggregate through it.

func (l *StorageLocation) ID() uuid.UUID          { return l.id }
func (l *StorageLocation) CompanyID() uuid.UUID   { return l.companyID }
func (l *StorageLocation) WarehouseID() uuid.UUID { return l.warehouseID }
func (l *StorageLocation) Version() uint64        { return l.version }
func (l *StorageLocation) Code() LocationCode     { return l.code }
func (l *StorageLocation) Coordinate() Coordinate { return l.coordinate }
func (l *StorageLocation) Barcode() Barcode       { return l.barcode }
func (l *StorageLocation) Status() Status         { return l.status }
func (l *StorageLocation) PickingPriority() int   { return l.pickingPriority }
func (l *StorageLocation) AllowMixedSKU() bool    { return l.allowMixedSKU }
func (l *StorageLocation) AllowOverflow() bool    { return l.allowOverflow }
func (l *StorageLocation) Capacity() Capacity     { return l.capacity }
func (l *StorageLocation) CreatedBy() uuid.UUID   { return l.createdBy }
func (l *StorageLocation) UpdatedBy() uuid.UUID   { return l.updatedBy }
func (l *StorageLocation) CreatedAt() time.Time   { return l.createdAt }
func (l *StorageLocation) UpdatedAt() time.Time   { return l.updatedAt }

// DeletedAt returns the archive timestamp, or nil.
func (l *StorageLocation) DeletedAt() *time.Time {
	if l.deletedAt == nil {
		return nil
	}
	at := *l.deletedAt
	return &at
}

// ---------- Domain predicates ----------

// IsArchived reports whether the location has been archived.
func (l *StorageLocation) IsArchived() bool { return l.deletedAt != nil }

// IsLocked reports whether the location is under an operational hold.
func (l *StorageLocation) IsLocked() bool { return l.status == StatusLocked }

// CanReceiveInventory reports whether stock may be put away here.
//
// This is the aggregate's half of the rule. Only an ACTIVE, unarchived location
// qualifies — a LOCKED, INACTIVE or MAINTENANCE location refuses.
//
// It is NOT the whole answer. Cross-aggregate conditions (is the product
// compatible? would this exceed capacity?) belong to service.ReceivingGuard,
// and the service composes the two. Expressing the local half here means every
// future caller inherits it rather than reimplementing a status comparison.
func (l *StorageLocation) CanReceiveInventory() bool {
	return l.status == StatusActive && !l.IsArchived()
}

// CanPickInventory reports whether stock may be taken from here.
//
// Deliberately more permissive than receiving: a MAINTENANCE location may still
// be picked FROM. Work is scheduled on a rack precisely so its remaining stock
// can be drained before the work starts, and blocking picks would strand it.
//
// A LOCKED location refuses both — a lock means "nobody touches this".
func (l *StorageLocation) CanPickInventory() bool {
	if l.IsArchived() || l.status == StatusLocked || l.status == StatusInactive {
		return false
	}
	return true
}

// BelongsTo reports whether the location is owned by the given tenant. It backs
// the defence-in-depth check in the service layer.
func (l *StorageLocation) BelongsTo(companyID uuid.UUID) bool {
	return l.companyID == companyID
}

// IsInWarehouse reports whether the location belongs to the given warehouse.
//
// Separate from BelongsTo because they answer different questions: a location
// can be in the right company and the wrong warehouse, which is exactly the
// mistake a bulk import makes.
func (l *StorageLocation) IsInWarehouse(warehouseID uuid.UUID) bool {
	return l.warehouseID == warehouseID
}

// ---------- Lifecycle ----------

// Activate returns the location to service.
//
// Reachable from INACTIVE and MAINTENANCE, but NOT from LOCKED. A lock is an
// operational hold with a reason, and lifting it is Unlock — a distinct
// decision that says "the problem is resolved". Allowing Activate to clear it
// would mean a routine reactivation silently discards a safety hold.
func (l *StorageLocation) Activate(actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("Activate"); err != nil {
		return err
	}

	if l.status == StatusActive {
		// Idempotent. A retried request is not a business failure, and
		// returning a conflict would make clients implement compensating logic
		// for a no-op.
		return nil
	}

	if l.status == StatusLocked {
		return apperror.Conflict("A locked location must be unlocked, not activated").
			WithOp("location.Activate").
			WithDetails(map[string]any{"current_status": l.status.String()})
	}

	l.status = StatusActive
	l.touch(actorID, now)

	return nil
}

// Deactivate stands the location down.
//
// Refused on a LOCKED location for the same reason as Activate: deactivating
// would replace a hold that has a reason with one that does not.
func (l *StorageLocation) Deactivate(actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("Deactivate"); err != nil {
		return err
	}

	if l.status == StatusInactive {
		return nil
	}

	if l.status == StatusLocked {
		return apperror.Conflict("A locked location must be unlocked before it can be deactivated").
			WithOp("location.Deactivate").
			WithDetails(map[string]any{"current_status": l.status.String()})
	}

	l.status = StatusInactive
	l.touch(actorID, now)

	return nil
}

// Lock places the location under an operational hold.
//
// Reachable from any live status. Damage, a spill or a stock discrepancy can
// affect a location whatever its current state, and refusing to record that
// would mean the hold simply is not represented.
//
// The reason is required. A lock nobody can explain is one nobody can safely
// lift — and the person lifting it is usually not the person who imposed it.
func (l *StorageLocation) Lock(reason string, actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("Lock"); err != nil {
		return err
	}

	trimmed := trimReason(reason)
	if trimmed == "" {
		return apperror.NewValidation(apperror.FieldError{
			Field: "reason", Rule: "required",
			Message: "a lock reason is required",
		}).WithOp("location.Lock")
	}

	if l.status == StatusLocked {
		return nil
	}

	previous := l.status
	l.status = StatusLocked
	l.touch(actorID, now)

	l.record(newEvent(EventLocationLocked, l, actorID, now).
		With("previous_status", previous.String()).
		With("reason", trimmed))

	return nil
}

// Unlock lifts an operational hold, returning the location to ACTIVE.
//
// Only a LOCKED location can be unlocked; calling it on anything else is a
// conflict rather than a no-op, because "unlock" on an unlocked location almost
// always means the caller targeted the wrong record.
func (l *StorageLocation) Unlock(actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("Unlock"); err != nil {
		return err
	}

	if l.status != StatusLocked {
		return apperror.Conflict("Only a locked location can be unlocked").
			WithOp("location.Unlock").
			WithDetails(map[string]any{"current_status": l.status.String()})
	}

	l.status = StatusActive
	l.touch(actorID, now)

	l.record(newEvent(EventLocationUnlocked, l, actorID, now))

	return nil
}

// StartMaintenance schedules work on the location.
//
// Refused on a LOCKED location: a lock is an unplanned hold, and converting it
// to planned maintenance would discard the reason.
func (l *StorageLocation) StartMaintenance(actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("StartMaintenance"); err != nil {
		return err
	}

	if l.status == StatusMaintenance {
		return nil
	}

	if l.status == StatusLocked {
		return apperror.Conflict("A locked location must be unlocked before maintenance").
			WithOp("location.StartMaintenance").
			WithDetails(map[string]any{"current_status": l.status.String()})
	}

	l.status = StatusMaintenance
	l.touch(actorID, now)

	return nil
}

// Archive retires the location.
//
// A location is never hard-deleted: future stock movements will reference it
// forever, and erasing the row would orphan the history of what was stored
// where — exactly what a stock investigation reads.
//
// CanArchive is checked first so the rule can be asked independently, for a
// client rendering a disabled button.
func (l *StorageLocation) Archive(actorID uuid.UUID, now time.Time) error {
	if err := l.CanArchive(); err != nil {
		return err
	}

	l.deletedAt = &now
	l.touch(actorID, now)

	return nil
}

// CanArchive reports whether the location may be archived.
//
// # Extension point
//
// This answers only what the AGGREGATE can see — currently "am I already
// archived?".
//
// It cannot answer "does it hold stock?", because stock is another aggregate.
// That check belongs to service.InventoryProvider, which the Inventory sprint
// implements; the service asks the provider, then asks the aggregate. Splitting
// it this way keeps this package free of every future module.
func (l *StorageLocation) CanArchive() error {
	if l.IsArchived() {
		return apperror.Conflict("This location is already archived").
			WithOp("location.CanArchive")
	}
	return nil
}

// ---------- Attribute changes ----------

// AssignBarcode attaches or replaces a scannable label.
//
// Uniqueness within the company is NOT checked here. An aggregate can only see
// itself, and "is this barcode taken?" is a question about a set — it belongs
// to the repository, which the service consults before calling this.
func (l *StorageLocation) AssignBarcode(barcode Barcode, actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("AssignBarcode"); err != nil {
		return err
	}

	if barcode.String() == l.barcode.String() {
		return nil
	}

	previous := l.barcode
	l.barcode = barcode
	l.touch(actorID, now)

	// The event records whether a label was REPLACED, which matters
	// operationally: a replaced barcode means an old label is still physically
	// on the rack and must be removed, while a first assignment does not.
	l.record(newEvent(EventBarcodeAssigned, l, actorID, now).
		With("barcode", barcode.String()).
		With("replaced", previous.IsPresent()))

	return nil
}

// ChangeCapacity updates what the location can hold.
//
// # The central business rule of this aggregate
//
// Capacity may not be reduced below what is currently stored. A bin holding 400
// kg cannot be re-declared as a 300 kg bin: the stock is physically there, and
// the system would immediately be reporting an overflow it has no way to
// resolve.
//
// The usage is passed IN rather than fetched. The aggregate cannot see stock —
// that is another aggregate — so the service obtains it from
// CurrentCapacityProvider and hands it over. The RULE stays in the domain; only
// the FACT comes from outside. That is the shape every cross-aggregate
// invariant in this codebase takes, and it is what keeps this method testable
// with no infrastructure at all.
//
// allowOverflow does not exempt a reduction. Overflow permits putaway to exceed
// a limit in the moment; it does not make a limit that is already exceeded
// acceptable to declare.
func (l *StorageLocation) ChangeCapacity(
	capacity Capacity, usage Usage, actorID uuid.UUID, now time.Time,
) error {
	if err := l.assertNotArchived("ChangeCapacity"); err != nil {
		return err
	}

	if !capacity.CanAccommodate(usage) {
		return apperror.Conflict(
			"Capacity cannot be reduced below the quantity currently stored here").
			WithOp("location.ChangeCapacity").
			WithDetails(map[string]any{
				"current_weight":  usage.Weight.String(),
				"current_volume":  usage.Volume.String(),
				"current_pallets": usage.Pallets,
			})
	}

	previous := l.capacity
	l.capacity = capacity
	l.touch(actorID, now)

	l.record(newEvent(EventCapacityChanged, l, actorID, now).
		With("previous_max_weight", previous.MaxWeight().String()).
		With("previous_max_volume", previous.MaxVolume().String()).
		With("max_weight", capacity.MaxWeight().String()).
		With("max_volume", capacity.MaxVolume().String()))

	return nil
}

// ChangePickingPriority reorders the location in a pick path.
func (l *StorageLocation) ChangePickingPriority(
	priority int, actorID uuid.UUID, now time.Time,
) error {
	if err := l.assertNotArchived("ChangePickingPriority"); err != nil {
		return err
	}

	if priority < 0 {
		return apperror.NewValidation(apperror.FieldError{
			Field: "picking_priority", Rule: "min",
			Message: "picking_priority must not be negative",
		}).WithOp("location.ChangePickingPriority")
	}

	if priority == l.pickingPriority {
		return nil
	}

	l.pickingPriority = priority
	l.touch(actorID, now)

	return nil
}

// EnableMixedSKU permits more than one SKU to occupy the location.
//
// Widening a rule, so it is always safe: whatever is already stored remains
// valid. The reverse is not — see DisableMixedSKU.
func (l *StorageLocation) EnableMixedSKU(actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("EnableMixedSKU"); err != nil {
		return err
	}

	if l.allowMixedSKU {
		return nil
	}

	l.allowMixedSKU = true
	l.touch(actorID, now)

	return nil
}

// DisableMixedSKU restricts the location to a single SKU.
//
// distinctSKUs is passed in for the same reason usage is passed to
// ChangeCapacity: the aggregate cannot count what is stored here. Narrowing a
// rule while it is already violated would leave the location permanently
// non-compliant with no way for the system to say so.
//
// A negative count is treated as "unknown" and permitted, which is what the
// permissive InventoryProvider returns until the Inventory module exists —
// blocking every call until then would make the operation unusable for no
// safety benefit.
func (l *StorageLocation) DisableMixedSKU(
	distinctSKUs int, actorID uuid.UUID, now time.Time,
) error {
	if err := l.assertNotArchived("DisableMixedSKU"); err != nil {
		return err
	}

	if !l.allowMixedSKU {
		return nil
	}

	if distinctSKUs > 1 {
		return apperror.Conflict(
			"Mixed SKUs cannot be disabled while more than one SKU is stored here").
			WithOp("location.DisableMixedSKU").
			WithDetails(map[string]any{"distinct_skus": distinctSKUs})
	}

	l.allowMixedSKU = false
	l.touch(actorID, now)

	return nil
}

// SetAllowOverflow controls whether putaway may exceed the declared capacity.
func (l *StorageLocation) SetAllowOverflow(allow bool, actorID uuid.UUID, now time.Time) error {
	if err := l.assertNotArchived("SetAllowOverflow"); err != nil {
		return err
	}

	if allow == l.allowOverflow {
		return nil
	}

	l.allowOverflow = allow
	l.touch(actorID, now)

	return nil
}

// ---------- Events ----------

// PullEvents returns the recorded events and clears them.
//
// "Pull" rather than a getter because the operation is destructive by design:
// the service publishes what it takes, and leaving them in place would mean a
// second save republishes the same events.
func (l *StorageLocation) PullEvents() []Event {
	events := l.events
	l.events = nil
	return events
}

// record appends a domain event.
func (l *StorageLocation) record(event Event) {
	l.events = append(l.events, event)
}

// ---------- internals ----------

// touch stamps the mutation audit fields.
func (l *StorageLocation) touch(actorID uuid.UUID, now time.Time) {
	l.updatedBy = actorID
	l.updatedAt = now
}

// assertNotArchived refuses any mutation of an archived location.
//
// Applied uniformly: an archived location is a historical record, and permitting
// even a priority change would mean the record of what the place was at
// retirement is not stable.
func (l *StorageLocation) assertNotArchived(op string) error {
	if l.IsArchived() {
		return apperror.Conflict("An archived location cannot be modified").
			WithOp("location." + op)
	}
	return nil
}
