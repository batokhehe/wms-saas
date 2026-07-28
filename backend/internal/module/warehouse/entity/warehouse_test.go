package entity

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// These tests exercise the aggregate with NO infrastructure at all: no
// database, no HTTP, no fakes. That is the practical payoff of keeping business
// rules inside the aggregate — the rules can be verified in microseconds, and a
// rule that needed a database to test would be a rule in the wrong layer.

func at(hour int) time.Time {
	return time.Date(2026, 7, 25, hour, 0, 0, 0, time.UTC)
}

// draft builds a DRAFT warehouse, the state every warehouse starts in.
func draft(t *testing.T) (*Warehouse, uuid.UUID) {
	t.Helper()

	code, err := NewCode("wh-01")
	if err != nil {
		t.Fatalf("NewCode() = %v", err)
	}

	actor := uuid.New()
	w, err := NewWarehouse(uuid.New(), uuid.New(), code,
		"Jakarta Central", "Primary DC", TypeMain, actor, at(10))
	if err != nil {
		t.Fatalf("NewWarehouse() = %v", err)
	}

	return w, actor
}

// ready builds a warehouse that satisfies every activation requirement.
func ready(t *testing.T) (*Warehouse, uuid.UUID) {
	t.Helper()

	w, actor := draft(t)

	address, _ := NewAddress("Jl. Sudirman 1, Jakarta")
	if err := w.ChangeAddress(address, actor, at(10)); err != nil {
		t.Fatalf("ChangeAddress() = %v", err)
	}

	contact, _ := NewContact("Budi", "+62-811-1111")
	if err := w.ChangeContact(contact, actor, at(10)); err != nil {
		t.Fatalf("ChangeContact() = %v", err)
	}

	if err := w.AssignReceivingZone(uuid.New(), actor, at(10)); err != nil {
		t.Fatalf("AssignReceivingZone() = %v", err)
	}

	w.PullEvents() // discard setup events so tests assert on their own
	return w, actor
}

// ---------- Construction ----------

func TestNewWarehouseStartsAsDraft(t *testing.T) {
	w, _ := draft(t)

	if w.Status() != StatusDraft {
		t.Errorf("status = %q, want DRAFT", w.Status())
	}
	// A brand-new warehouse must not be able to move stock. If construction
	// could produce an ACTIVE warehouse, every activation rule would be
	// bypassable by simply creating one.
	if w.CanReceiveInventory() || w.CanShipInventory() {
		t.Error("a DRAFT warehouse reported itself operational")
	}
}

func TestNewWarehouseRaisesCreatedEvent(t *testing.T) {
	w, _ := draft(t)

	events := w.PullEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Name != EventWarehouseCreated {
		t.Errorf("event = %q, want %q", events[0].Name, EventWarehouseCreated)
	}
	if events[0].WarehouseID != w.ID() || events[0].CompanyID != w.CompanyID() {
		t.Error("the event does not identify its aggregate and tenant")
	}
}

// TestPullEventsClears is what makes double publication impossible: the service
// publishes what it pulls, and a second save must not republish.
func TestPullEventsClears(t *testing.T) {
	w, _ := draft(t)

	if len(w.PullEvents()) != 1 {
		t.Fatal("first pull returned no events")
	}
	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("second pull returned %d events, want 0", len(got))
	}
}

// TestReconstituteRaisesNoEvents is the DDD distinction that is easiest to get
// wrong. Loading a row is not a business event — if it raised one, an audit log
// would claim the warehouse was created once per page view.
func TestReconstituteRaisesNoEvents(t *testing.T) {
	code, _ := NewCode("WH-01")
	address, _ := NewAddress("somewhere")
	contact, _ := NewContact("Budi", "+62-811")

	w := Reconstitute(
		uuid.New(), uuid.New(), code, "Jakarta", "", TypeMain, StatusActive,
		address, contact, NewZones(nil, nil, nil),
		uuid.New(), uuid.New(), at(9), at(9), nil,
	)

	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("Reconstitute raised %d events, want 0", len(got))
	}
	if w.Status() != StatusActive {
		t.Errorf("status = %q, want the stored ACTIVE", w.Status())
	}
}

func TestNewWarehouseRejectsInvalidInput(t *testing.T) {
	code, _ := NewCode("WH-01")

	tests := map[string]struct {
		name          string
		warehouseType Type
	}{
		"name too short": {"A", TypeMain},
		"blank name":     {"   ", TypeMain},
		"unknown type":   {"Valid Name", Type("DEPOT")},
	}

	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			_, err := NewWarehouse(uuid.New(), uuid.New(), code,
				tt.name, "", tt.warehouseType, uuid.New(), at(10))
			if err == nil {
				t.Fatal("NewWarehouse() = nil, want a validation error")
			}
			if code := apperror.From(err).Code; code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", code)
			}
		})
	}
}

// ---------- Activation ----------

// TestActivateRequiresEverything is the sprint's central business rule. Every
// missing requirement is reported together, so an operator completing a
// warehouse sees the whole checklist rather than discovering it one rejection
// at a time.
func TestActivateRequiresEverything(t *testing.T) {
	w, actor := draft(t)

	err := w.Activate(actor, at(11))
	if err == nil {
		t.Fatal("Activate() = nil for an incomplete warehouse")
	}

	appErr := apperror.From(err)
	if appErr.Code != apperror.CodeValidation {
		t.Fatalf("code = %s, want VALIDATION_ERROR", appErr.Code)
	}

	details, ok := appErr.Details.(apperror.ValidationDetails)
	if !ok {
		t.Fatalf("details = %#v, want ValidationDetails", appErr.Details)
	}

	reported := map[string]bool{}
	for _, field := range details.Fields {
		reported[field.Field] = true
	}

	for _, want := range []string{"address", "contact", "zones"} {
		if !reported[want] {
			t.Errorf("activation did not report the missing %s; got %+v", want, details.Fields)
		}
	}

	// The status must be unchanged after a rejected activation.
	if w.Status() != StatusDraft {
		t.Errorf("status = %q after a failed activation, want DRAFT", w.Status())
	}
}

func TestActivateSucceedsWhenReady(t *testing.T) {
	w, actor := ready(t)

	if err := w.Activate(actor, at(11)); err != nil {
		t.Fatalf("Activate() = %v", err)
	}

	if w.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", w.Status())
	}
	if !w.CanReceiveInventory() || !w.CanShipInventory() {
		t.Error("an ACTIVE warehouse reported itself unable to move stock")
	}

	events := w.PullEvents()
	if len(events) != 1 || events[0].Name != EventWarehouseActivated {
		t.Errorf("events = %+v, want one WarehouseActivated", events)
	}
	if events[0].Attributes["previous_status"] != StatusDraft.String() {
		t.Errorf("previous_status = %v, want DRAFT", events[0].Attributes["previous_status"])
	}
}

// TestActivateIsIdempotent: a retried request is not a business failure, and
// returning a conflict would make clients implement compensating logic for a
// no-op.
func TestActivateIsIdempotent(t *testing.T) {
	w, actor := ready(t)

	if err := w.Activate(actor, at(11)); err != nil {
		t.Fatalf("first Activate() = %v", err)
	}
	w.PullEvents()

	if err := w.Activate(actor, at(12)); err != nil {
		t.Errorf("second Activate() = %v, want nil", err)
	}
	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("a no-op activation raised %d events, want 0", len(got))
	}
}

// TestActivateAcceptsAnySingleZone: "at least one" rather than "all three",
// because a TRANSIT cross-dock legitimately has no staging area.
func TestActivateAcceptsAnySingleZone(t *testing.T) {
	assignments := map[string]func(*Warehouse, uuid.UUID, uuid.UUID, time.Time) error{
		"receiving": (*Warehouse).AssignReceivingZone,
		"shipping":  (*Warehouse).AssignShippingZone,
		"staging":   (*Warehouse).AssignStagingZone,
	}

	for label, assign := range assignments {
		t.Run(label, func(t *testing.T) {
			w, actor := draft(t)

			address, _ := NewAddress("Jl. Sudirman 1")
			_ = w.ChangeAddress(address, actor, at(10))
			contact, _ := NewContact("Budi", "+62-811")
			_ = w.ChangeContact(contact, actor, at(10))

			if err := assign(w, uuid.New(), actor, at(10)); err != nil {
				t.Fatalf("assigning %s zone: %v", label, err)
			}

			if err := w.Activate(actor, at(11)); err != nil {
				t.Errorf("Activate() with only a %s zone = %v, want nil", label, err)
			}
		})
	}
}

// ---------- Lifecycle transitions ----------

func TestDeactivateOnlyFromActive(t *testing.T) {
	w, actor := ready(t)

	// DRAFT cannot be deactivated: it was never active.
	err := w.Deactivate(actor, at(11))
	if err == nil {
		t.Fatal("Deactivate() = nil for a DRAFT warehouse")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	_ = w.Activate(actor, at(11))
	if err := w.Deactivate(actor, at(12)); err != nil {
		t.Errorf("Deactivate() from ACTIVE = %v, want nil", err)
	}
	if w.Status() != StatusInactive {
		t.Errorf("status = %q, want INACTIVE", w.Status())
	}
}

// TestDeactivateCannotLiftASuspension guards the governance boundary: turning a
// hold into an operational state would silently discard the reason it was
// imposed.
func TestDeactivateCannotLiftASuspension(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))

	if err := w.Suspend("failed fire inspection", actor, at(12)); err != nil {
		t.Fatalf("Suspend() = %v", err)
	}

	if err := w.Deactivate(actor, at(13)); err == nil {
		t.Fatal("Deactivate() lifted a suspension")
	}
	if w.Status() != StatusSuspended {
		t.Errorf("status = %q, want it still SUSPENDED", w.Status())
	}
}

func TestSuspendRequiresAReason(t *testing.T) {
	w, actor := ready(t)

	err := w.Suspend("   ", actor, at(11))
	if err == nil {
		t.Fatal("Suspend() = nil with a blank reason")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
	if w.Status() == StatusSuspended {
		t.Error("the warehouse was suspended despite the rejection")
	}
}

// TestSuspendReachableFromAnyLiveStatus: a compliance block can land on a site
// that was never commissioned, and refusing to record that would mean the block
// simply is not represented.
func TestSuspendReachableFromAnyLiveStatus(t *testing.T) {
	t.Run("from draft", func(t *testing.T) {
		w, actor := draft(t)
		if err := w.Suspend("audit hold", actor, at(11)); err != nil {
			t.Errorf("Suspend() from DRAFT = %v, want nil", err)
		}
	})

	t.Run("from active", func(t *testing.T) {
		w, actor := ready(t)
		_ = w.Activate(actor, at(11))
		if err := w.Suspend("audit hold", actor, at(12)); err != nil {
			t.Errorf("Suspend() from ACTIVE = %v, want nil", err)
		}
	})
}

func TestSuspendedWarehouseCannotMoveStock(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))
	_ = w.Suspend("audit hold", actor, at(12))

	if w.CanReceiveInventory() || w.CanShipInventory() {
		t.Error("a SUSPENDED warehouse reported itself able to move stock")
	}
}

func TestSuspendRaisesEventWithReason(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))
	w.PullEvents()

	_ = w.Suspend("failed fire inspection", actor, at(12))

	events := w.PullEvents()
	if len(events) != 1 || events[0].Name != EventWarehouseSuspended {
		t.Fatalf("events = %+v, want one WarehouseSuspended", events)
	}
	if events[0].Attributes["reason"] != "failed fire inspection" {
		t.Errorf("reason = %v, want it recorded", events[0].Attributes["reason"])
	}
}

// ---------- Archiving ----------

func TestArchiveIsSoftAndFinal(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))
	w.PullEvents()

	if err := w.Archive(actor, at(12)); err != nil {
		t.Fatalf("Archive() = %v", err)
	}

	if !w.IsArchived() {
		t.Error("IsArchived() = false after Archive()")
	}
	if w.DeletedAt() == nil {
		t.Error("DeletedAt() is nil after Archive()")
	}
	// An archived warehouse must not move stock, whatever its status was.
	if w.CanReceiveInventory() || w.CanShipInventory() {
		t.Error("an archived warehouse reported itself operational")
	}

	events := w.PullEvents()
	if len(events) != 1 || events[0].Name != EventWarehouseArchived {
		t.Errorf("events = %+v, want one WarehouseArchived", events)
	}
}

func TestArchiveTwiceIsConflict(t *testing.T) {
	w, actor := ready(t)
	_ = w.Archive(actor, at(12))

	err := w.Archive(actor, at(13))
	if err == nil {
		t.Fatal("second Archive() = nil")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestArchivedWarehouseIsImmutable: an archived warehouse is a historical
// record, and permitting even a description edit would mean the record of what
// the site was at retirement is not stable.
func TestArchivedWarehouseIsImmutable(t *testing.T) {
	w, actor := ready(t)
	_ = w.Archive(actor, at(12))

	address, _ := NewAddress("new address")
	contact, _ := NewContact("Someone", "+62-999")

	mutations := map[string]error{
		"ChangeName":        w.ChangeName("Renamed", actor, at(13)),
		"ChangeDescription": w.ChangeDescription("new", actor, at(13)),
		"ChangeAddress":     w.ChangeAddress(address, actor, at(13)),
		"ChangeContact":     w.ChangeContact(contact, actor, at(13)),
		"Activate":          w.Activate(actor, at(13)),
		"Deactivate":        w.Deactivate(actor, at(13)),
		"Suspend":           w.Suspend("reason", actor, at(13)),
		"AssignZone":        w.AssignReceivingZone(uuid.New(), actor, at(13)),
	}

	for name, err := range mutations {
		if err == nil {
			t.Errorf("%s succeeded on an archived warehouse", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeConflict {
			t.Errorf("%s: code = %s, want CONFLICT", name, code)
		}
	}
}

// TestCanArchiveIsAnIndependentQuestion: a client rendering a disabled button
// needs the answer without attempting the operation.
func TestCanArchiveIsAnIndependentQuestion(t *testing.T) {
	w, actor := ready(t)

	if err := w.CanArchive(); err != nil {
		t.Errorf("CanArchive() = %v for a live warehouse, want nil", err)
	}

	_ = w.Archive(actor, at(12))

	if err := w.CanArchive(); err == nil {
		t.Error("CanArchive() = nil for an already-archived warehouse")
	}
}

// ---------- Attribute invariants ----------

// TestActiveWarehouseKeepsItsAddress closes the back door: activation demanded
// an address, so removing it afterwards would leave an operational site no
// driver can reach.
func TestActiveWarehouseKeepsItsAddress(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))

	empty, _ := NewAddress("")
	err := w.ChangeAddress(empty, actor, at(12))
	if err == nil {
		t.Fatal("cleared the address of an ACTIVE warehouse")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
	if !w.Address().IsPresent() {
		t.Error("the address was cleared despite the rejection")
	}
}

func TestActiveWarehouseKeepsItsContact(t *testing.T) {
	w, actor := ready(t)
	_ = w.Activate(actor, at(11))

	empty, _ := NewContact("", "")
	if err := w.ChangeContact(empty, actor, at(12)); err == nil {
		t.Fatal("cleared the contact of an ACTIVE warehouse")
	}
	if !w.Contact().IsPresent() {
		t.Error("the contact was cleared despite the rejection")
	}
}

// TestDraftWarehouseMayClearAddress is the counterpart: the invariant applies
// to ACTIVE warehouses, not to drafts still being filled in.
func TestDraftWarehouseMayClearAddress(t *testing.T) {
	w, actor := draft(t)

	address, _ := NewAddress("temporary")
	_ = w.ChangeAddress(address, actor, at(10))

	empty, _ := NewAddress("")
	if err := w.ChangeAddress(empty, actor, at(11)); err != nil {
		t.Errorf("clearing a DRAFT warehouse's address = %v, want nil", err)
	}
}

// TestChangeContactRaisesEvent: the contact is who gets called when a delivery
// goes wrong, so a silent change is an operational risk. Renames are cosmetic
// and deliberately raise nothing.
func TestChangeContactRaisesEventButRenameDoesNot(t *testing.T) {
	w, actor := ready(t)

	contact, _ := NewContact("Siti", "+62-822-2222")
	if err := w.ChangeContact(contact, actor, at(11)); err != nil {
		t.Fatalf("ChangeContact() = %v", err)
	}

	events := w.PullEvents()
	if len(events) != 1 || events[0].Name != EventWarehouseContactChanged {
		t.Fatalf("events = %+v, want one WarehouseContactChanged", events)
	}
	if events[0].Attributes["previous_contact_name"] != "Budi" {
		t.Errorf("previous_contact_name = %v, want Budi",
			events[0].Attributes["previous_contact_name"])
	}
	// The previous phone is personal data and must not be in an event stream.
	if _, leaked := events[0].Attributes["previous_contact_phone"]; leaked {
		t.Error("the previous phone number leaked into the event")
	}

	if err := w.ChangeName("Renamed", actor, at(12)); err != nil {
		t.Fatalf("ChangeName() = %v", err)
	}
	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("a rename raised %d events, want 0", len(got))
	}
}

// TestNoOpChangesRaiseNoEvents keeps the audit stream free of noise from
// clients that resend unchanged values.
func TestNoOpChangesRaiseNoEvents(t *testing.T) {
	w, actor := ready(t)

	sameContact, _ := NewContact("Budi", "+62-811-1111")
	if err := w.ChangeContact(sameContact, actor, at(11)); err != nil {
		t.Fatalf("ChangeContact() = %v", err)
	}
	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("an unchanged contact raised %d events, want 0", len(got))
	}
}

// ---------- Zones ----------

func TestAssignZoneRaisesEvent(t *testing.T) {
	w, actor := draft(t)
	w.PullEvents()

	zoneID := uuid.New()
	if err := w.AssignShippingZone(zoneID, actor, at(11)); err != nil {
		t.Fatalf("AssignShippingZone() = %v", err)
	}

	events := w.PullEvents()
	if len(events) != 1 || events[0].Name != EventWarehouseZoneAssigned {
		t.Fatalf("events = %+v, want one WarehouseZoneAssigned", events)
	}
	if events[0].Attributes["zone_kind"] != ZoneShipping.String() {
		t.Errorf("zone_kind = %v, want SHIPPING", events[0].Attributes["zone_kind"])
	}

	if got := w.Zones().Shipping(); got == nil || *got != zoneID {
		t.Errorf("shipping zone = %v, want %s", got, zoneID)
	}
}

func TestAssignZoneRejectsNilID(t *testing.T) {
	w, actor := draft(t)

	err := w.AssignReceivingZone(uuid.Nil, actor, at(11))
	if err == nil {
		t.Fatal("AssignReceivingZone(uuid.Nil) = nil")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
}

func TestReassigningTheSameZoneIsANoOp(t *testing.T) {
	w, actor := draft(t)
	zoneID := uuid.New()

	_ = w.AssignReceivingZone(zoneID, actor, at(11))
	w.PullEvents()

	_ = w.AssignReceivingZone(zoneID, actor, at(12))
	if got := w.PullEvents(); len(got) != 0 {
		t.Errorf("reassigning the same zone raised %d events, want 0", len(got))
	}
}

// ---------- Encapsulation ----------

// TestGettersReturnCopies proves a caller holding a value object cannot reach
// back into the aggregate through it — the property that makes the invariants
// enforceable at all.
func TestGettersReturnCopies(t *testing.T) {
	w, actor := ready(t)
	_ = w.Archive(actor, at(12))

	stolen := w.DeletedAt()
	if stolen == nil {
		t.Fatal("DeletedAt() = nil after Archive()")
	}

	// Mutating the returned pointer must not affect the aggregate.
	*stolen = at(23)

	if w.DeletedAt().Equal(at(23)) {
		t.Error("mutating the returned pointer changed the aggregate's timestamp")
	}
}

// TestZonesValueObjectIsImmutable: Assign returns a copy, so a caller holding a
// Zones cannot alter the aggregate's own.
func TestZonesValueObjectIsImmutable(t *testing.T) {
	w, actor := draft(t)
	_ = w.AssignReceivingZone(uuid.New(), actor, at(11))

	stolen := w.Zones()
	stolen = stolen.Assign(ZoneShipping, uuid.New())

	if w.Zones().Shipping() != nil {
		t.Error("mutating a returned Zones changed the aggregate")
	}
	if stolen.Shipping() == nil {
		t.Error("Assign did not return the modified copy")
	}
}

// TestBelongsTo backs the service's defence-in-depth tenant check.
func TestBelongsTo(t *testing.T) {
	w, _ := draft(t)

	if !w.BelongsTo(w.CompanyID()) {
		t.Error("BelongsTo(own company) = false")
	}
	if w.BelongsTo(uuid.New()) {
		t.Error("BelongsTo(another company) = true")
	}
}

// ---------- Value objects ----------

func TestNewCodeValidates(t *testing.T) {
	valid := []string{"WH-01", "wh-01", " WH-01 ", "A1", "MAIN-JAKARTA-001"}
	for _, raw := range valid {
		if _, err := NewCode(raw); err != nil {
			t.Errorf("NewCode(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{"", "  ", "A", "-WH", "WH 01", "WH_01", "WH@01"}
	for _, raw := range invalid {
		if _, err := NewCode(raw); err == nil {
			t.Errorf("NewCode(%q) = nil, want a validation error", raw)
		}
	}
}

func TestNewCodeCanonicalises(t *testing.T) {
	code, err := NewCode("  wh-01  ")
	if err != nil {
		t.Fatalf("NewCode() = %v", err)
	}
	if code.String() != "WH-01" {
		t.Errorf("code = %q, want WH-01", code.String())
	}
}

// TestContactRejectsHalfStates: a name with no number gives an operator someone
// to blame and no way to call them.
func TestContactRejectsHalfStates(t *testing.T) {
	if _, err := NewContact("Budi", ""); err == nil {
		t.Error("NewContact(name, no phone) = nil")
	}
	if _, err := NewContact("", "+62-811"); err == nil {
		t.Error("NewContact(no name, phone) = nil")
	}

	// Both empty is legitimate — a DRAFT warehouse has no contact yet.
	contact, err := NewContact("", "")
	if err != nil {
		t.Errorf("NewContact(empty, empty) = %v, want nil", err)
	}
	if contact.IsPresent() {
		t.Error("an empty contact reported itself present")
	}
}

func TestZonesHasAnyAndCount(t *testing.T) {
	empty := NewZones(nil, nil, nil)
	if empty.HasAny() || empty.Count() != 0 {
		t.Error("an empty Zones reported assignments")
	}

	id := uuid.New()
	one := NewZones(&id, nil, nil)
	if !one.HasAny() || one.Count() != 1 {
		t.Errorf("HasAny=%t Count=%d, want true/1", one.HasAny(), one.Count())
	}
}

func TestStatusAndTypeValidity(t *testing.T) {
	for _, s := range []Status{StatusDraft, StatusActive, StatusInactive, StatusSuspended} {
		if !s.Valid() {
			t.Errorf("status %q reported invalid", s)
		}
	}
	if Status("ARCHIVED").Valid() {
		t.Error("an unknown status reported valid")
	}

	for _, ty := range []Type{TypeMain, TypeBranch, TypeTransit, TypeConsignment} {
		if !ty.Valid() {
			t.Errorf("type %q reported invalid", ty)
		}
	}
	if Type("DEPOT").Valid() {
		t.Error("an unknown type reported valid")
	}
}

// TestErrorsAreTyped: every domain rejection must be an apperror the transport
// layer can map, never a bare error that becomes a 500.
func TestErrorsAreTyped(t *testing.T) {
	w, actor := draft(t)

	err := w.Activate(actor, at(11))

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is %T, want *apperror.Error", err)
	}
	if appErr.Status < 400 || appErr.Status >= 500 {
		t.Errorf("status = %d, want a 4xx", appErr.Status)
	}
}
