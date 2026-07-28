package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// These tests exercise the aggregate with NO infrastructure at all. That is the
// payoff of keeping business rules inside it — including the capacity rule,
// which depends on external data yet is still testable here because the fact is
// PASSED IN rather than fetched.

func at(hour int) time.Time {
	return time.Date(2026, 7, 26, hour, 0, 0, 0, time.UTC)
}

func mustCoordinate(t *testing.T, zone, aisle, rack, level, bin string) Coordinate {
	t.Helper()
	c, err := NewCoordinate(zone, aisle, rack, level, bin)
	if err != nil {
		t.Fatalf("NewCoordinate() = %v", err)
	}
	return c
}

// newLocation builds an ACTIVE location, the state every location starts in.
func newLocation(t *testing.T) (*StorageLocation, uuid.UUID) {
	t.Helper()

	actor := uuid.New()
	l, err := NewStorageLocation(
		uuid.New(), uuid.New(), uuid.New(),
		mustCoordinate(t, "A", "01", "02", "03", ""),
		LocationCode{}, actor, at(10),
	)
	if err != nil {
		t.Fatalf("NewStorageLocation() = %v", err)
	}

	l.PullEvents() // discard the creation event so tests assert on their own
	return l, actor
}

func mustQuantity(t *testing.T, raw string) Quantity {
	t.Helper()
	q, err := NewQuantity(raw, "test")
	if err != nil {
		t.Fatalf("NewQuantity(%q) = %v", raw, err)
	}
	return q
}

func intPtr(v int) *int { return &v }

// ---------- Construction ----------

func TestNewLocationStartsActive(t *testing.T) {
	l, _ := newLocation(t)

	// Unlike Warehouse, which starts in DRAFT: a location has no commissioning
	// prerequisite. Once its coordinate exists, the physical shelf exists.
	if l.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", l.Status())
	}
	if !l.CanReceiveInventory() || !l.CanPickInventory() {
		t.Error("a new location reported itself unusable")
	}
	if l.PickingPriority() != DefaultPickingPriority {
		t.Errorf("priority = %d, want %d", l.PickingPriority(), DefaultPickingPriority)
	}
	if !l.Capacity().IsUnlimited() {
		t.Error("a new location has capacity limits it was never given")
	}
}

// TestCodeDefaultsToCoordinate: an operator reading "A-01-02-03" off a label
// knows where to walk, with nothing to look up.
func TestCodeDefaultsToCoordinate(t *testing.T) {
	l, _ := newLocation(t)

	if got, want := l.Code().String(), "A-01-02-03"; got != want {
		t.Errorf("code = %q, want %q derived from the coordinate", got, want)
	}
}

func TestExplicitCodeOverridesCoordinate(t *testing.T) {
	code, err := NewLocationCode("dock-1")
	if err != nil {
		t.Fatalf("NewLocationCode() = %v", err)
	}

	l, err := NewStorageLocation(
		uuid.New(), uuid.New(), uuid.New(),
		mustCoordinate(t, "RECV", "", "", "", ""),
		code, uuid.New(), at(10),
	)
	if err != nil {
		t.Fatalf("NewStorageLocation() = %v", err)
	}

	if got := l.Code().String(); got != "DOCK-1" {
		t.Errorf("code = %q, want the explicit DOCK-1", got)
	}
}

func TestNewLocationRaisesCreatedEvent(t *testing.T) {
	l, err := NewStorageLocation(
		uuid.New(), uuid.New(), uuid.New(),
		mustCoordinate(t, "A", "01", "", "", ""),
		LocationCode{}, uuid.New(), at(10),
	)
	if err != nil {
		t.Fatalf("NewStorageLocation() = %v", err)
	}

	events := l.PullEvents()
	if len(events) != 1 || events[0].Name != EventLocationCreated {
		t.Fatalf("events = %+v, want one LocationCreated", events)
	}
	// The event carries the warehouse so a consumer need not reload the
	// location to know which site is affected.
	if events[0].WarehouseID != l.WarehouseID() || events[0].CompanyID != l.CompanyID() {
		t.Error("the event does not identify its warehouse and tenant")
	}
}

// TestReconstituteRaisesNoEvents: loading a row is not a business event. A
// warehouse listing 500 locations must not publish 500 creations.
func TestReconstituteRaisesNoEvents(t *testing.T) {
	code, _ := NewLocationCode("A-01")

	l := Reconstitute(
		uuid.New(), uuid.New(), uuid.New(), code,
		mustCoordinate(t, "A", "01", "", "", ""),
		Barcode{}, StatusLocked, 50, true, true, UnlimitedCapacity(),
		uuid.New(), uuid.New(), at(9), at(9), nil,
	)

	if got := l.PullEvents(); len(got) != 0 {
		t.Errorf("Reconstitute raised %d events, want 0", len(got))
	}
	if l.Status() != StatusLocked {
		t.Errorf("status = %q, want the stored LOCKED", l.Status())
	}
}

func TestNewLocationRequiresACoordinate(t *testing.T) {
	_, err := NewStorageLocation(
		uuid.New(), uuid.New(), uuid.New(),
		Coordinate{}, LocationCode{}, uuid.New(), at(10),
	)
	if err == nil {
		t.Fatal("NewStorageLocation() = nil with no coordinate")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
}

// ---------- Coordinate ----------

func TestCoordinateRequiresZone(t *testing.T) {
	if _, err := NewCoordinate("", "01", "", "", ""); err == nil {
		t.Error("NewCoordinate() = nil with no zone")
	}
}

// TestCoordinateRejectsGaps: a location with a rack but no aisle describes no
// physical place and would sort nonsensically in a pick path.
func TestCoordinateRejectsGaps(t *testing.T) {
	tests := map[string][5]string{
		"rack without aisle": {"A", "", "02", "", ""},
		"bin without level":  {"A", "01", "02", "", "04"},
		"level without rack": {"A", "01", "", "03", ""},
	}

	for label, parts := range tests {
		t.Run(label, func(t *testing.T) {
			_, err := NewCoordinate(parts[0], parts[1], parts[2], parts[3], parts[4])
			if err == nil {
				t.Fatal("NewCoordinate() = nil for a gapped coordinate")
			}
			if code := apperror.From(err).Code; code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", code)
			}
		})
	}
}

// TestCoordinateAllowsPartialDepth: a floor stack or a dock genuinely has a
// zone and nothing else.
func TestCoordinateAllowsPartialDepth(t *testing.T) {
	tests := map[string][5]string{
		"zone only":       {"RECV", "", "", "", ""},
		"zone and aisle":  {"A", "01", "", "", ""},
		"full coordinate": {"A", "01", "02", "03", "04"},
	}

	for label, parts := range tests {
		t.Run(label, func(t *testing.T) {
			c, err := NewCoordinate(parts[0], parts[1], parts[2], parts[3], parts[4])
			if err != nil {
				t.Fatalf("NewCoordinate() = %v", err)
			}
			if c.Depth() == 0 {
				t.Error("depth = 0 for a valid coordinate")
			}
		})
	}
}

func TestCoordinateCanonicalises(t *testing.T) {
	c := mustCoordinate(t, " a ", "01", "", "", "")

	if c.Zone() != "A" {
		t.Errorf("zone = %q, want it upper-cased and trimmed", c.Zone())
	}
	if got, want := c.String(), "A-01"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if c.Depth() != 2 {
		t.Errorf("depth = %d, want 2", c.Depth())
	}
}

// ---------- Availability ----------

// TestReceivingAndPickingDifferByStatus is the asymmetry that matters: a
// MAINTENANCE location may be picked FROM so its stock can be drained before
// work starts, but must not be received INTO.
func TestReceivingAndPickingDifferByStatus(t *testing.T) {
	tests := map[Status]struct {
		canReceive bool
		canPick    bool
	}{
		StatusActive:      {true, true},
		StatusInactive:    {false, false},
		StatusLocked:      {false, false},
		StatusMaintenance: {false, true},
	}

	for status, want := range tests {
		t.Run(status.String(), func(t *testing.T) {
			code, _ := NewLocationCode("A-01")
			l := Reconstitute(
				uuid.New(), uuid.New(), uuid.New(), code,
				mustCoordinate(t, "A", "01", "", "", ""),
				Barcode{}, status, 100, false, false, UnlimitedCapacity(),
				uuid.New(), uuid.New(), at(9), at(9), nil,
			)

			if got := l.CanReceiveInventory(); got != want.canReceive {
				t.Errorf("CanReceiveInventory() = %t, want %t", got, want.canReceive)
			}
			if got := l.CanPickInventory(); got != want.canPick {
				t.Errorf("CanPickInventory() = %t, want %t", got, want.canPick)
			}
		})
	}
}

func TestArchivedLocationIsUnusable(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.Archive(actor, at(11)); err != nil {
		t.Fatalf("Archive() = %v", err)
	}

	if l.CanReceiveInventory() || l.CanPickInventory() {
		t.Error("an archived location reported itself usable")
	}
}

// ---------- Lock / Unlock ----------

func TestLockRequiresAReason(t *testing.T) {
	l, actor := newLocation(t)

	err := l.Lock("   ", actor, at(11))
	if err == nil {
		t.Fatal("Lock() = nil with a blank reason")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
	if l.IsLocked() {
		t.Error("the location was locked despite the rejection")
	}
}

func TestLockRaisesEventWithReason(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.Lock("damaged racking", actor, at(11)); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	events := l.PullEvents()
	if len(events) != 1 || events[0].Name != EventLocationLocked {
		t.Fatalf("events = %+v, want one LocationLocked", events)
	}
	if events[0].Attributes["reason"] != "damaged racking" {
		t.Errorf("reason = %v, want it recorded", events[0].Attributes["reason"])
	}
	if events[0].Attributes["previous_status"] != StatusActive.String() {
		t.Errorf("previous_status = %v, want ACTIVE", events[0].Attributes["previous_status"])
	}

	if l.CanReceiveInventory() || l.CanPickInventory() {
		t.Error("a LOCKED location reported itself usable")
	}
}

func TestLockIsIdempotent(t *testing.T) {
	l, actor := newLocation(t)

	_ = l.Lock("damaged racking", actor, at(11))
	l.PullEvents()

	if err := l.Lock("damaged racking", actor, at(12)); err != nil {
		t.Errorf("second Lock() = %v, want nil", err)
	}
	if got := l.PullEvents(); len(got) != 0 {
		t.Errorf("a no-op lock raised %d events, want 0", len(got))
	}
}

func TestUnlockReturnsToActive(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.Lock("spill", actor, at(11))
	l.PullEvents()

	if err := l.Unlock(actor, at(12)); err != nil {
		t.Fatalf("Unlock() = %v", err)
	}

	if l.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", l.Status())
	}

	events := l.PullEvents()
	if len(events) != 1 || events[0].Name != EventLocationUnlocked {
		t.Errorf("events = %+v, want one LocationUnlocked", events)
	}
}

// TestUnlockOnlyFromLocked: "unlock" on an unlocked location almost always
// means the caller targeted the wrong record.
func TestUnlockOnlyFromLocked(t *testing.T) {
	l, actor := newLocation(t)

	err := l.Unlock(actor, at(11))
	if err == nil {
		t.Fatal("Unlock() = nil for an ACTIVE location")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestLockedLocationRefusesOtherTransitions is the governance boundary: a lock
// has a reason, and letting Activate or Deactivate clear it would discard that
// reason silently.
func TestLockedLocationRefusesOtherTransitions(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.Lock("failed inspection", actor, at(11))

	transitions := map[string]error{
		"Activate":         l.Activate(actor, at(12)),
		"Deactivate":       l.Deactivate(actor, at(12)),
		"StartMaintenance": l.StartMaintenance(actor, at(12)),
	}

	for name, err := range transitions {
		if err == nil {
			t.Errorf("%s cleared a lock", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeConflict {
			t.Errorf("%s: code = %s, want CONFLICT", name, code)
		}
	}

	if !l.IsLocked() {
		t.Error("the location is no longer locked")
	}
}

// ---------- Activate / Deactivate / Maintenance ----------

func TestActivateFromInactiveAndMaintenance(t *testing.T) {
	for _, from := range []Status{StatusInactive, StatusMaintenance} {
		t.Run(from.String(), func(t *testing.T) {
			l, actor := newLocation(t)

			switch from {
			case StatusInactive:
				_ = l.Deactivate(actor, at(11))
			case StatusMaintenance:
				_ = l.StartMaintenance(actor, at(11))
			}

			if err := l.Activate(actor, at(12)); err != nil {
				t.Fatalf("Activate() from %s = %v", from, err)
			}
			if l.Status() != StatusActive {
				t.Errorf("status = %q, want ACTIVE", l.Status())
			}
		})
	}
}

func TestActivateIsIdempotent(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.Activate(actor, at(11)); err != nil {
		t.Errorf("Activate() on an ACTIVE location = %v, want nil", err)
	}
}

func TestMaintenanceAllowsPickingButNotReceiving(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.StartMaintenance(actor, at(11)); err != nil {
		t.Fatalf("StartMaintenance() = %v", err)
	}

	if l.CanReceiveInventory() {
		t.Error("a MAINTENANCE location accepted receiving")
	}
	if !l.CanPickInventory() {
		t.Error("a MAINTENANCE location refused picking; its stock would be stranded")
	}
}

// ---------- Capacity ----------

// TestCapacityCannotBeReducedBelowUsage is the aggregate's central rule.
//
// Note that the usage is PASSED IN. The aggregate cannot see stock — that is
// another aggregate — so the service fetches the fact and hands it over. The
// rule stays in the domain, which is why this test needs no infrastructure.
func TestCapacityCannotBeReducedBelowUsage(t *testing.T) {
	l, actor := newLocation(t)

	// Start at 500 kg with 400 kg stored.
	initial, _ := NewCapacity(mustQuantity(t, "500"), Quantity{}, nil)
	usage := Usage{Weight: mustQuantity(t, "400")}

	if err := l.ChangeCapacity(initial, usage, actor, at(11)); err != nil {
		t.Fatalf("ChangeCapacity(500) = %v", err)
	}
	l.PullEvents()

	// Reducing to 300 kg would leave the location instantly over capacity.
	reduced, _ := NewCapacity(mustQuantity(t, "300"), Quantity{}, nil)

	err := l.ChangeCapacity(reduced, usage, actor, at(12))
	if err == nil {
		t.Fatal("capacity was reduced below what is stored")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	// The limit must be unchanged after a rejection.
	if got := l.Capacity().MaxWeight().String(); got != "500.000" {
		t.Errorf("max_weight = %q, want it unchanged at 500", got)
	}
	if got := l.PullEvents(); len(got) != 0 {
		t.Errorf("a rejected change raised %d events, want 0", len(got))
	}
}

func TestCapacityCanBeReducedToExactUsage(t *testing.T) {
	l, actor := newLocation(t)

	usage := Usage{Weight: mustQuantity(t, "400")}
	exact, _ := NewCapacity(mustQuantity(t, "400"), Quantity{}, nil)

	if err := l.ChangeCapacity(exact, usage, actor, at(11)); err != nil {
		t.Errorf("reducing to exactly the stored quantity = %v, want nil", err)
	}
}

// TestCapacityCanAlwaysBeWidened: clearing a limit means "not measured", which
// is a widening rather than a narrowing.
func TestCapacityCanAlwaysBeWidened(t *testing.T) {
	l, actor := newLocation(t)

	usage := Usage{Weight: mustQuantity(t, "400")}

	limited, _ := NewCapacity(mustQuantity(t, "500"), Quantity{}, nil)
	_ = l.ChangeCapacity(limited, usage, actor, at(11))

	if err := l.ChangeCapacity(UnlimitedCapacity(), usage, actor, at(12)); err != nil {
		t.Errorf("clearing a limit = %v, want nil", err)
	}
	if !l.Capacity().IsUnlimited() {
		t.Error("the capacity is still limited")
	}
}

func TestCapacityChecksEachDimensionIndependently(t *testing.T) {
	l, actor := newLocation(t)

	usage := Usage{
		Weight:  mustQuantity(t, "100"),
		Volume:  mustQuantity(t, "50"),
		Pallets: intPtr(4),
	}

	tests := map[string]Capacity{
		"weight too low": mustCapacity(t, "50", "", nil),
		"volume too low": mustCapacity(t, "", "10", nil),
		"pallets too low": func() Capacity {
			c, _ := NewCapacity(Quantity{}, Quantity{}, intPtr(2))
			return c
		}(),
	}

	for label, capacity := range tests {
		t.Run(label, func(t *testing.T) {
			if err := l.ChangeCapacity(capacity, usage, actor, at(11)); err == nil {
				t.Error("a capacity below usage in one dimension was accepted")
			}
		})
	}
}

// TestOverflowDoesNotExemptReduction: overflow permits putaway to exceed a
// limit in the moment; it does not make a limit that is already exceeded
// acceptable to declare.
func TestOverflowDoesNotExemptReduction(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.SetAllowOverflow(true, actor, at(10)); err != nil {
		t.Fatalf("SetAllowOverflow() = %v", err)
	}

	usage := Usage{Weight: mustQuantity(t, "400")}
	reduced, _ := NewCapacity(mustQuantity(t, "300"), Quantity{}, nil)

	if err := l.ChangeCapacity(reduced, usage, actor, at(11)); err == nil {
		t.Error("allow_overflow exempted a capacity reduction below usage")
	}
}

func TestCapacityChangeRaisesEvent(t *testing.T) {
	l, actor := newLocation(t)

	capacity, _ := NewCapacity(mustQuantity(t, "1000"), mustQuantity(t, "2.5"), intPtr(4))

	if err := l.ChangeCapacity(capacity, Usage{}, actor, at(11)); err != nil {
		t.Fatalf("ChangeCapacity() = %v", err)
	}

	events := l.PullEvents()
	if len(events) != 1 || events[0].Name != EventCapacityChanged {
		t.Fatalf("events = %+v, want one CapacityChanged", events)
	}
	if events[0].Attributes["max_weight"] != "1000.000" {
		t.Errorf("max_weight = %v, want 1000.000", events[0].Attributes["max_weight"])
	}
}

// TestQuantityIsExact guards the decision to use big.Rat rather than float64.
// A WMS adds and subtracts these thousands of times a day.
func TestQuantityIsExact(t *testing.T) {
	q := mustQuantity(t, "1234.567")

	if got := q.String(); got != "1234.567" {
		t.Errorf("String() = %q, want 1234.567 with no rounding", got)
	}

	// 0.1 + 0.2 in binary floating point is 0.30000000000000004.
	a := mustQuantity(t, "0.1")
	b := mustQuantity(t, "0.3")
	if !a.LessThan(b) {
		t.Error("0.1 < 0.3 reported false")
	}
}

func TestQuantityRejectsNegative(t *testing.T) {
	if _, err := NewQuantity("-1", "max_weight"); err == nil {
		t.Error("NewQuantity(-1) = nil")
	}
	if _, err := NewQuantity("not-a-number", "max_weight"); err == nil {
		t.Error("NewQuantity(garbage) = nil")
	}
}

// TestUnsetCapacityIsUnbounded: absence differs from zero. An unmeasured bin
// accepts stock; a zero-capacity bin accepts none.
func TestUnsetCapacityIsUnbounded(t *testing.T) {
	unlimited := UnlimitedCapacity()
	heavy := Usage{Weight: mustQuantity(t, "999999")}

	if !unlimited.CanAccommodate(heavy) {
		t.Error("an unlimited capacity refused a large usage")
	}

	zero, _ := NewCapacity(mustQuantity(t, "0"), Quantity{}, nil)
	if zero.CanAccommodate(heavy) {
		t.Error("a zero capacity accepted a large usage")
	}
}

// ---------- Mixed SKU ----------

// TestDisableMixedSKUBlockedWhileMultipleSKUsStored: narrowing a rule while it
// is already violated would leave the location permanently non-compliant.
func TestDisableMixedSKUBlockedWhileMultipleSKUsStored(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.EnableMixedSKU(actor, at(10))

	err := l.DisableMixedSKU(3, actor, at(11))
	if err == nil {
		t.Fatal("mixed SKUs were disabled while three SKUs are stored")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
	if !l.AllowMixedSKU() {
		t.Error("the flag changed despite the rejection")
	}
}

func TestDisableMixedSKUAllowedWithOneOrNone(t *testing.T) {
	for _, count := range []int{0, 1} {
		l, actor := newLocation(t)
		_ = l.EnableMixedSKU(actor, at(10))

		if err := l.DisableMixedSKU(count, actor, at(11)); err != nil {
			t.Errorf("DisableMixedSKU(%d) = %v, want nil", count, err)
		}
	}
}

// TestUnknownSKUCountIsPermissive: a negative count means "unknown", which is
// what the permissive provider returns until Inventory exists. Blocking every
// call until then would make the operation unusable for no safety benefit.
func TestUnknownSKUCountIsPermissive(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.EnableMixedSKU(actor, at(10))

	if err := l.DisableMixedSKU(-1, actor, at(11)); err != nil {
		t.Errorf("DisableMixedSKU(unknown) = %v, want nil", err)
	}
}

// TestEnableMixedSKUNeedsNoFact: widening a rule is always safe, so no
// cross-aggregate lookup is needed.
func TestEnableMixedSKUNeedsNoFact(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.EnableMixedSKU(actor, at(11)); err != nil {
		t.Errorf("EnableMixedSKU() = %v, want nil", err)
	}
	if !l.AllowMixedSKU() {
		t.Error("the flag was not set")
	}
}

// ---------- Barcode ----------

func TestAssignBarcodeRaisesEvent(t *testing.T) {
	l, actor := newLocation(t)

	barcode, err := NewBarcode("LOC-000123")
	if err != nil {
		t.Fatalf("NewBarcode() = %v", err)
	}

	if err := l.AssignBarcode(barcode, actor, at(11)); err != nil {
		t.Fatalf("AssignBarcode() = %v", err)
	}

	events := l.PullEvents()
	if len(events) != 1 || events[0].Name != EventBarcodeAssigned {
		t.Fatalf("events = %+v, want one BarcodeAssigned", events)
	}
	// A first assignment is not a replacement — the distinction matters because
	// a replaced label is still physically on the rack.
	if events[0].Attributes["replaced"] != false {
		t.Errorf("replaced = %v, want false for a first assignment",
			events[0].Attributes["replaced"])
	}
}

func TestReplacingBarcodeIsFlagged(t *testing.T) {
	l, actor := newLocation(t)

	first, _ := NewBarcode("LOC-000123")
	_ = l.AssignBarcode(first, actor, at(11))
	l.PullEvents()

	second, _ := NewBarcode("LOC-000999")
	_ = l.AssignBarcode(second, actor, at(12))

	events := l.PullEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Attributes["replaced"] != true {
		t.Error("replaced = false for a replacement; the old label is still on the rack")
	}
}

// TestBarcodePreservesCase: a barcode is a machine token reproduced exactly by
// a scanner, unlike a code or coordinate.
func TestBarcodePreservesCase(t *testing.T) {
	barcode, err := NewBarcode("Loc-AbC123")
	if err != nil {
		t.Fatalf("NewBarcode() = %v", err)
	}
	if got := barcode.String(); got != "Loc-AbC123" {
		t.Errorf("barcode = %q, want its case preserved", got)
	}
}

func TestEmptyBarcodeIsNotAnError(t *testing.T) {
	barcode, err := NewBarcode("  ")
	if err != nil {
		t.Errorf("NewBarcode(empty) = %v, want nil", err)
	}
	if barcode.IsPresent() {
		t.Error("an empty barcode reported itself present")
	}
}

func TestBarcodeRejectsMalformed(t *testing.T) {
	for _, raw := range []string{"ab", "has space", "semi;colon"} {
		if _, err := NewBarcode(raw); err == nil {
			t.Errorf("NewBarcode(%q) = nil, want a validation error", raw)
		}
	}
}

// ---------- Archiving and immutability ----------

func TestArchiveIsSoftAndFinal(t *testing.T) {
	l, actor := newLocation(t)

	if err := l.Archive(actor, at(11)); err != nil {
		t.Fatalf("Archive() = %v", err)
	}
	if !l.IsArchived() || l.DeletedAt() == nil {
		t.Error("the location does not report itself archived")
	}

	if err := l.Archive(actor, at(12)); err == nil {
		t.Fatal("second Archive() = nil")
	}
}

// TestArchivedLocationIsImmutable: an archived location is a historical record,
// and permitting even a priority change would mean the record of what the place
// was at retirement is not stable.
func TestArchivedLocationIsImmutable(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.Archive(actor, at(11))

	barcode, _ := NewBarcode("LOC-000123")

	mutations := map[string]error{
		"Activate":              l.Activate(actor, at(12)),
		"Deactivate":            l.Deactivate(actor, at(12)),
		"Lock":                  l.Lock("reason", actor, at(12)),
		"Unlock":                l.Unlock(actor, at(12)),
		"StartMaintenance":      l.StartMaintenance(actor, at(12)),
		"AssignBarcode":         l.AssignBarcode(barcode, actor, at(12)),
		"ChangeCapacity":        l.ChangeCapacity(UnlimitedCapacity(), Usage{}, actor, at(12)),
		"ChangePickingPriority": l.ChangePickingPriority(1, actor, at(12)),
		"EnableMixedSKU":        l.EnableMixedSKU(actor, at(12)),
		"SetAllowOverflow":      l.SetAllowOverflow(true, actor, at(12)),
	}

	for name, err := range mutations {
		if err == nil {
			t.Errorf("%s succeeded on an archived location", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeConflict {
			t.Errorf("%s: code = %s, want CONFLICT", name, code)
		}
	}
}

// ---------- Encapsulation ----------

// TestGettersReturnCopies proves a caller holding a value cannot reach back into
// the aggregate through it — the property that makes the invariants enforceable.
func TestGettersReturnCopies(t *testing.T) {
	l, actor := newLocation(t)

	capacity, _ := NewCapacity(Quantity{}, Quantity{}, intPtr(10))
	_ = l.ChangeCapacity(capacity, Usage{}, actor, at(11))

	stolen := l.Capacity().MaxPallet()
	if stolen == nil {
		t.Fatal("MaxPallet() = nil after it was set")
	}
	*stolen = 999

	if got := l.Capacity().MaxPallet(); got == nil || *got != 10 {
		t.Errorf("max_pallet = %v after mutating the returned pointer, want 10", got)
	}

	_ = l.Archive(actor, at(12))
	archivedAt := l.DeletedAt()
	*archivedAt = at(23)

	if l.DeletedAt().Equal(at(23)) {
		t.Error("mutating the returned timestamp changed the aggregate")
	}
}

func TestBelongsToAndIsInWarehouse(t *testing.T) {
	l, _ := newLocation(t)

	if !l.BelongsTo(l.CompanyID()) {
		t.Error("BelongsTo(own company) = false")
	}
	if l.BelongsTo(uuid.New()) {
		t.Error("BelongsTo(another company) = true")
	}

	// A separate question: a location can be in the right company and the wrong
	// warehouse, which is exactly the mistake a bulk import makes.
	if !l.IsInWarehouse(l.WarehouseID()) {
		t.Error("IsInWarehouse(own warehouse) = false")
	}
	if l.IsInWarehouse(uuid.New()) {
		t.Error("IsInWarehouse(another warehouse) = true")
	}
}

// TestNoOpChangesRaiseNoEvents keeps the audit stream free of noise from clients
// that resend unchanged values.
func TestNoOpChangesRaiseNoEvents(t *testing.T) {
	l, actor := newLocation(t)

	barcode, _ := NewBarcode("LOC-000123")
	_ = l.AssignBarcode(barcode, actor, at(11))
	l.PullEvents()

	same, _ := NewBarcode("LOC-000123")
	_ = l.AssignBarcode(same, actor, at(12))

	if got := l.PullEvents(); len(got) != 0 {
		t.Errorf("reassigning the same barcode raised %d events, want 0", len(got))
	}
}

// TestPullEventsClears makes double publication impossible rather than merely
// unlikely.
func TestPullEventsClears(t *testing.T) {
	l, actor := newLocation(t)
	_ = l.Lock("reason", actor, at(11))

	if len(l.PullEvents()) != 1 {
		t.Fatal("first pull returned no events")
	}
	if got := l.PullEvents(); len(got) != 0 {
		t.Errorf("second pull returned %d events, want 0", len(got))
	}
}

// ---------- helpers ----------

func mustCapacity(t *testing.T, weight, volume string, pallets *int) Capacity {
	t.Helper()

	w, err := NewQuantity(weight, "max_weight")
	if err != nil {
		t.Fatalf("NewQuantity(%q) = %v", weight, err)
	}
	v, err := NewQuantity(volume, "max_volume")
	if err != nil {
		t.Fatalf("NewQuantity(%q) = %v", volume, err)
	}

	c, err := NewCapacity(w, v, pallets)
	if err != nil {
		t.Fatalf("NewCapacity() = %v", err)
	}
	return c
}
