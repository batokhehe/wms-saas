package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Warehouse is the aggregate root of the warehouse domain.
//
// # Why every field is unexported
//
// This is the first type in the codebase that does NOT embed
// shared/entity.BaseEntity and carries no GORM tags, and the departure is the
// whole point of the sprint.
//
// An aggregate root owns its invariants. "Only an ACTIVE warehouse may ship"
// and "activation requires an address, a contact and a zone" are only true if
// there is no way to reach ACTIVE except through Activate(). Exported fields
// make that unenforceable: any caller can write `w.Status = StatusActive` and
// every rule in this file becomes advisory.
//
// So the fields are unexported, there are no setters, and the only way to
// change state is a method that states a business intent. Reading is via
// getters, which return copies for value objects.
//
// The cost is that GORM cannot map this type — it reflects over exported
// fields. That is paid in repository/model.go with an explicit persistence
// model and a translation, which is the standard shape for DDD over an ORM.
// EntityConvention still governs that model; it governs persistence types, and
// this is not one.
type Warehouse struct {
	id        uuid.UUID
	companyID uuid.UUID
	version   uint64

	code          Code
	name          string
	description   string
	warehouseType Type
	status        Status

	address Address
	contact Contact
	zones   Zones

	createdBy uuid.UUID
	updatedBy uuid.UUID

	createdAt time.Time
	updatedAt time.Time
	deletedAt *time.Time

	// events accumulates what happened to this aggregate during the current
	// unit of work. The aggregate records; the service publishes.
	//
	// That split matters: an aggregate that published directly would need a
	// publisher dependency, which would drag infrastructure into the innermost
	// layer and make every domain test require a broker.
	events []Event
}

// ---------- Construction ----------

// NewWarehouse creates a warehouse in DRAFT.
//
// It is the ONLY way to bring a warehouse into existence, and it always starts
// in DRAFT — never ACTIVE. A caller cannot create an already-operational
// warehouse, because doing so would bypass the activation rules entirely.
//
// Address, contact and zones are absent by design: they are supplied later
// through their own intent-revealing methods.
func NewWarehouse(
	id uuid.UUID,
	companyID uuid.UUID,
	code Code,
	name string,
	description string,
	warehouseType Type,
	actorID uuid.UUID,
	now time.Time,
) (*Warehouse, error) {
	normalizedName, err := normalizeName(name)
	if err != nil {
		return nil, err
	}

	if code.IsZero() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "code", Rule: "required", Message: "code is required",
		}).WithOp("warehouse.NewWarehouse")
	}

	if !warehouseType.Valid() {
		return nil, apperror.NewValidation(apperror.FieldError{
			Field: "type", Rule: "oneof",
			Message: "type must be one of: MAIN, BRANCH, TRANSIT, CONSIGNMENT",
		}).WithOp("warehouse.NewWarehouse")
	}

	w := &Warehouse{
		id:            id,
		companyID:     companyID,
		version:       1,
		code:          code,
		name:          normalizedName,
		description:   strings.TrimSpace(description),
		warehouseType: warehouseType,
		status:        StatusDraft,
		createdBy:     actorID,
		updatedBy:     actorID,
		createdAt:     now,
		updatedAt:     now,
	}

	w.record(newEvent(EventWarehouseCreated, w, actorID, now).
		With("code", code.String()).
		With("type", warehouseType.String()))

	return w, nil
}

// Reconstitute rebuilds a warehouse from storage WITHOUT raising events.
//
// The distinction from NewWarehouse is fundamental to DDD and easy to get
// wrong. Loading a row is not a business event: if reconstitution raised
// WarehouseCreated, every read would publish a creation, and an audit log would
// claim the warehouse was created once per page view.
//
// It is exported because the repository must call it, and it performs no
// validation — the data came from the database, which already enforced the
// constraints. Validating here would mean a row that became invalid through a
// migration could never be loaded, not even to be fixed.
func Reconstitute(
	id, companyID uuid.UUID,
	code Code,
	name, description string,
	warehouseType Type,
	status Status,
	address Address,
	contact Contact,
	zones Zones,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
	deletedAt *time.Time,
) *Warehouse {
	return ReconstituteWithVersion(id, companyID, 1, code, name, description, warehouseType,
		status, address, contact, zones, createdBy, updatedBy, createdAt, updatedAt, deletedAt)
}

// ReconstituteWithVersion rebuilds a persisted aggregate with its repository-
// owned concurrency token. Domain behaviour never changes this value.
func ReconstituteWithVersion(
	id, companyID uuid.UUID,
	version uint64,
	code Code,
	name, description string,
	warehouseType Type,
	status Status,
	address Address,
	contact Contact,
	zones Zones,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
	deletedAt *time.Time,
) *Warehouse {
	return &Warehouse{
		id:            id,
		companyID:     companyID,
		version:       version,
		code:          code,
		name:          name,
		description:   description,
		warehouseType: warehouseType,
		status:        status,
		address:       address,
		contact:       contact,
		zones:         zones,
		createdBy:     createdBy,
		updatedBy:     updatedBy,
		createdAt:     createdAt,
		updatedAt:     updatedAt,
		deletedAt:     deletedAt,
	}
}

// ---------- Getters ----------
//
// Read access only. Value objects are returned by value, so a caller holding
// one cannot reach back into the aggregate through it.

func (w *Warehouse) ID() uuid.UUID        { return w.id }
func (w *Warehouse) CompanyID() uuid.UUID { return w.companyID }
func (w *Warehouse) Version() uint64      { return w.version }
func (w *Warehouse) Code() Code           { return w.code }
func (w *Warehouse) Name() string         { return w.name }
func (w *Warehouse) Description() string  { return w.description }
func (w *Warehouse) Type() Type           { return w.warehouseType }
func (w *Warehouse) Status() Status       { return w.status }
func (w *Warehouse) Address() Address     { return w.address }
func (w *Warehouse) Contact() Contact     { return w.contact }
func (w *Warehouse) Zones() Zones         { return w.zones }
func (w *Warehouse) CreatedBy() uuid.UUID { return w.createdBy }
func (w *Warehouse) UpdatedBy() uuid.UUID { return w.updatedBy }
func (w *Warehouse) CreatedAt() time.Time { return w.createdAt }
func (w *Warehouse) UpdatedAt() time.Time { return w.updatedAt }

// DeletedAt returns the archive timestamp, or nil.
func (w *Warehouse) DeletedAt() *time.Time {
	if w.deletedAt == nil {
		return nil
	}
	// A copy, so a caller cannot mutate the aggregate's own timestamp through
	// the returned pointer.
	at := *w.deletedAt
	return &at
}

// ---------- Domain predicates ----------

// IsArchived reports whether the warehouse has been archived.
func (w *Warehouse) IsArchived() bool { return w.deletedAt != nil }

// IsOperational reports whether the warehouse is ACTIVE.
func (w *Warehouse) IsOperational() bool {
	return w.status == StatusActive && !w.IsArchived()
}

// CanReceiveInventory reports whether goods may be booked in here.
//
// This is one of the two rules future modules will consult, and it is expressed
// as a question the aggregate answers rather than a status comparison the
// caller performs. When "receiving requires a receiving zone" becomes true, it
// changes here and every caller inherits it.
func (w *Warehouse) CanReceiveInventory() bool { return w.IsOperational() }

// CanShipInventory reports whether goods may be dispatched from here.
func (w *Warehouse) CanShipInventory() bool { return w.IsOperational() }

// BelongsTo reports whether the warehouse is owned by the given tenant. It
// backs the defence-in-depth check in the service layer.
func (w *Warehouse) BelongsTo(companyID uuid.UUID) bool { return w.companyID == companyID }

// ---------- Lifecycle ----------

// Activate makes the warehouse operational.
//
// This is where the sprint's central business rule lives. Activation is a
// declaration that the site is fit to receive and ship stock, so it demands
// that the facts an operator needs are actually present:
//
//   - a name (always true — enforced at construction)
//   - an address, so a driver can reach it
//   - a contact, so someone can be called when a delivery goes wrong
//   - at least one operational zone, so arriving goods have somewhere to go
//
// Every missing requirement is reported, not just the first: an operator
// completing a warehouse should see the whole remaining checklist rather than
// discovering it one rejection at a time.
func (w *Warehouse) Activate(actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("Activate"); err != nil {
		return err
	}

	if w.status == StatusActive {
		// Idempotent rather than an error. A retried request, or two operators
		// pressing the button at once, is not a business failure — and
		// returning a conflict would make clients implement compensating logic
		// for a no-op.
		return nil
	}

	if err := w.assertReadyForActivation(); err != nil {
		return err
	}

	previous := w.status
	w.status = StatusActive
	w.touch(actorID, now)

	w.record(newEvent(EventWarehouseActivated, w, actorID, now).
		With("previous_status", previous.String()))

	return nil
}

// assertReadyForActivation collects every unmet activation requirement.
func (w *Warehouse) assertReadyForActivation() error {
	var fields []apperror.FieldError

	if strings.TrimSpace(w.name) == "" {
		fields = append(fields, apperror.FieldError{
			Field: "name", Rule: "required",
			Message: "a warehouse must have a name before it can be activated",
		})
	}
	if !w.address.IsPresent() {
		fields = append(fields, apperror.FieldError{
			Field: "address", Rule: "required",
			Message: "a warehouse must have an address before it can be activated",
		})
	}
	if !w.contact.IsPresent() {
		fields = append(fields, apperror.FieldError{
			Field: "contact", Rule: "required",
			Message: "a warehouse must have a contact name and phone before it can be activated",
		})
	}
	if !w.zones.HasAny() {
		fields = append(fields, apperror.FieldError{
			Field: "zones", Rule: "required",
			Message: "a warehouse must have at least one operational zone assigned before it can be activated",
		})
	}

	if len(fields) > 0 {
		return apperror.NewValidation(fields...).WithOp("warehouse.Activate")
	}

	return nil
}

// Deactivate stands the warehouse down.
//
// Only an ACTIVE warehouse can be deactivated. A DRAFT one has never been
// active, and a SUSPENDED one is under a hold that deactivating would silently
// lift — turning a governance state into an operational one and losing the
// reason it was imposed.
func (w *Warehouse) Deactivate(actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("Deactivate"); err != nil {
		return err
	}

	if w.status == StatusInactive {
		return nil
	}

	if w.status != StatusActive {
		return apperror.Conflict("Only an active warehouse can be deactivated").
			WithOp("warehouse.Deactivate").
			WithDetails(map[string]any{"current_status": w.status.String()})
	}

	w.status = StatusInactive
	w.touch(actorID, now)

	return nil
}

// Suspend places the warehouse under a hold.
//
// Reachable from any live status including DRAFT: a compliance block can land
// on a site that was never commissioned, and refusing to record that would mean
// the block simply is not represented.
//
// The reason is required. A suspension with no reason is unactionable — nobody
// downstream can tell whether it is safe to lift.
func (w *Warehouse) Suspend(reason string, actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("Suspend"); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return apperror.NewValidation(apperror.FieldError{
			Field: "reason", Rule: "required",
			Message: "a suspension reason is required",
		}).WithOp("warehouse.Suspend")
	}

	if w.status == StatusSuspended {
		return nil
	}

	previous := w.status
	w.status = StatusSuspended
	w.touch(actorID, now)

	w.record(newEvent(EventWarehouseSuspended, w, actorID, now).
		With("previous_status", previous.String()).
		With("reason", trimmed))

	return nil
}

// Archive retires the warehouse.
//
// This is the ONLY removal operation the domain offers. A warehouse is never
// hard-deleted: future stock movements, receipts and shipments will reference
// it forever, and erasing the row would orphan years of operational history
// that an audit depends on.
//
// CanArchive is checked first so the rule is stated once and can be asked
// independently — a client rendering a disabled "archive" button needs the
// answer without attempting the operation.
func (w *Warehouse) Archive(actorID uuid.UUID, now time.Time) error {
	if err := w.CanArchive(); err != nil {
		return err
	}

	w.deletedAt = &now
	w.touch(actorID, now)

	w.record(newEvent(EventWarehouseArchived, w, actorID, now).
		With("status_at_archive", w.status.String()))

	return nil
}

// CanArchive reports whether the warehouse may be archived.
//
// # Extension point
//
// This answers only the questions the AGGREGATE can answer — currently just
// "is it already archived?".
//
// It cannot answer "does it hold stock?", because stock lives in another
// aggregate and an aggregate must not reach across that boundary to load one.
// That check belongs to a domain service; see service.DeletionGuard, which the
// Inventory sprint implements. The two compose: the service asks the guard,
// then asks the aggregate.
//
// Splitting it this way is what keeps this package free of every future module.
func (w *Warehouse) CanArchive() error {
	if w.IsArchived() {
		return apperror.Conflict("This warehouse is already archived").
			WithOp("warehouse.CanArchive")
	}
	return nil
}

// ---------- Attribute changes ----------

// ChangeName renames the warehouse.
//
// Uniqueness within the company is NOT checked here. An aggregate can only see
// itself, and "is this name taken?" is a question about a set — it belongs to
// the repository, which the service consults before calling this. Pretending
// otherwise would mean loading every sibling warehouse into memory to answer it.
func (w *Warehouse) ChangeName(name string, actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("ChangeName"); err != nil {
		return err
	}

	normalized, err := normalizeName(name)
	if err != nil {
		return err
	}

	if normalized == w.name {
		return nil
	}

	w.name = normalized
	w.touch(actorID, now)

	return nil
}

// ChangeDescription updates the free-text description.
func (w *Warehouse) ChangeDescription(description string, actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("ChangeDescription"); err != nil {
		return err
	}

	trimmed := strings.TrimSpace(description)
	if trimmed == w.description {
		return nil
	}

	w.description = trimmed
	w.touch(actorID, now)

	return nil
}

// ChangeAddress relocates the warehouse.
//
// Clearing the address of an ACTIVE warehouse is refused. Activation required
// one, so permitting its removal afterwards would leave an operational site
// that no driver can reach — an invariant broken through the back door.
func (w *Warehouse) ChangeAddress(address Address, actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("ChangeAddress"); err != nil {
		return err
	}

	if !address.IsPresent() && w.status == StatusActive {
		return apperror.Conflict("An active warehouse must have an address").
			WithOp("warehouse.ChangeAddress")
	}

	if address.String() == w.address.String() {
		return nil
	}

	w.address = address
	w.touch(actorID, now)

	return nil
}

// ChangeContact updates who is responsible for the warehouse.
//
// Like the address, an ACTIVE warehouse may not have its contact removed.
//
// This raises an event where ChangeName and ChangeAddress do not, and the
// asymmetry is deliberate: the contact is who gets called when a delivery goes
// wrong, so a silent change to it is an operational risk that downstream
// systems — notification routing, escalation policies — need to know about. A
// renamed warehouse is a cosmetic change nobody must react to.
func (w *Warehouse) ChangeContact(contact Contact, actorID uuid.UUID, now time.Time) error {
	if err := w.assertNotArchived("ChangeContact"); err != nil {
		return err
	}

	if !contact.IsPresent() && w.status == StatusActive {
		return apperror.Conflict("An active warehouse must have a contact").
			WithOp("warehouse.ChangeContact")
	}

	if contact.Name() == w.contact.Name() && contact.Phone() == w.contact.Phone() {
		return nil
	}

	previous := w.contact
	w.contact = contact
	w.touch(actorID, now)

	// The previous NAME is recorded but not the previous phone number: an audit
	// reader needs to know who was replaced, and a phone number is personal data
	// that does not belong in an event stream.
	w.record(newEvent(EventWarehouseContactChanged, w, actorID, now).
		With("previous_contact_name", previous.Name()).
		With("contact_name", contact.Name()))

	return nil
}

// ---------- Zone assignment ----------

// AssignReceivingZone sets where inbound goods arrive.
func (w *Warehouse) AssignReceivingZone(zoneID uuid.UUID, actorID uuid.UUID, now time.Time) error {
	return w.assignZone(ZoneReceiving, zoneID, actorID, now)
}

// AssignShippingZone sets where outbound goods depart.
func (w *Warehouse) AssignShippingZone(zoneID uuid.UUID, actorID uuid.UUID, now time.Time) error {
	return w.assignZone(ZoneShipping, zoneID, actorID, now)
}

// AssignStagingZone sets where goods rest between arrival and departure.
func (w *Warehouse) AssignStagingZone(zoneID uuid.UUID, actorID uuid.UUID, now time.Time) error {
	return w.assignZone(ZoneStaging, zoneID, actorID, now)
}

// assignZone is the shared implementation.
//
// It cannot verify the zone EXISTS — that is another aggregate, and the
// Location module does not exist. The service layer's ZoneVerifier is the
// extension point for that check; see service/guards.go.
func (w *Warehouse) assignZone(
	kind ZoneKind, zoneID uuid.UUID, actorID uuid.UUID, now time.Time,
) error {
	if err := w.assertNotArchived("AssignZone"); err != nil {
		return err
	}

	if !kind.Valid() {
		return apperror.NewValidation(apperror.FieldError{
			Field: "zone_kind", Rule: "oneof",
			Message: "zone kind must be one of: RECEIVING, SHIPPING, STAGING",
		}).WithOp("warehouse.assignZone")
	}

	if zoneID == uuid.Nil {
		return apperror.NewValidation(apperror.FieldError{
			Field: "zone_id", Rule: "required",
			Message: "a zone id is required",
		}).WithOp("warehouse.assignZone")
	}

	if existing := w.zones.Get(kind); existing != nil && *existing == zoneID {
		return nil
	}

	w.zones = w.zones.Assign(kind, zoneID)
	w.touch(actorID, now)

	w.record(newEvent(EventWarehouseZoneAssigned, w, actorID, now).
		With("zone_kind", kind.String()).
		With("zone_id", zoneID.String()))

	return nil
}

// ---------- Events ----------

// PullEvents returns the recorded events and clears them.
//
// "Pull" rather than a getter because the operation is destructive by design:
// the service publishes what it takes, and leaving them in place would mean a
// second save republishes the same events. Clearing on read makes double
// publication impossible rather than merely unlikely.
func (w *Warehouse) PullEvents() []Event {
	events := w.events
	w.events = nil
	return events
}

// record appends a domain event.
func (w *Warehouse) record(event Event) {
	w.events = append(w.events, event)
}

// ---------- internals ----------

// touch stamps the mutation audit fields. Every state change calls it, so
// "who last changed this and when" is never left stale.
func (w *Warehouse) touch(actorID uuid.UUID, now time.Time) {
	w.updatedBy = actorID
	w.updatedAt = now
}

// assertNotArchived refuses any mutation of an archived warehouse.
//
// Applied uniformly rather than per-method: an archived warehouse is a
// historical record, and permitting even a description edit would mean the
// record of what the site was at retirement is not stable.
func (w *Warehouse) assertNotArchived(op string) error {
	if w.IsArchived() {
		return apperror.Conflict("An archived warehouse cannot be modified").
			WithOp("warehouse." + op)
	}
	return nil
}

// normalizeName trims and validates a warehouse name.
func normalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	const minNameLength, maxNameLength = 2, 255

	if len(name) < minNameLength {
		return "", apperror.NewValidation(apperror.FieldError{
			Field: "name", Rule: "min",
			Message: "name must be at least 2 characters",
		}).WithOp("warehouse.normalizeName")
	}
	if len(name) > maxNameLength {
		return "", apperror.NewValidation(apperror.FieldError{
			Field: "name", Rule: "max",
			Message: "name must be at most 255 characters",
		}).WithOp("warehouse.normalizeName")
	}

	return name, nil
}
