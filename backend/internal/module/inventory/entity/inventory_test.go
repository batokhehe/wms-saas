package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func act() (uuid.UUID, time.Time) { return uuid.New(), time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC) }

// newNone builds an ACTIVE, untracked record with the given on-hand.
func newNone(t *testing.T, onHand int64) *Inventory {
	t.Helper()
	actor, now := act()
	inv, err := NewInventory(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		TrackingNone, NoLotNumber(), NoSerialNumber(), MustInventoryQuantity(onHand), actor, now)
	if err != nil {
		t.Fatalf("NewInventory(NONE) = %v", err)
	}
	inv.PullEvents() // discard the creation event for a clean slate
	return inv
}

// newLot builds an ACTIVE, lot-tracked record.
func newLot(t *testing.T, lot string, onHand int64) *Inventory {
	t.Helper()
	actor, now := act()
	lotNo, err := NewLotNumber(lot)
	if err != nil {
		t.Fatalf("NewLotNumber = %v", err)
	}
	inv, err := NewInventory(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		TrackingLot, lotNo, NoSerialNumber(), MustInventoryQuantity(onHand), actor, now)
	if err != nil {
		t.Fatalf("NewInventory(LOT) = %v", err)
	}
	inv.PullEvents()
	return inv
}

// newSerial builds an ACTIVE, serial-tracked record with on-hand exactly one.
func newSerial(t *testing.T, serial string) *Inventory {
	t.Helper()
	actor, now := act()
	serialNo, err := NewSerialNumber(serial)
	if err != nil {
		t.Fatalf("NewSerialNumber = %v", err)
	}
	inv, err := NewInventory(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		TrackingSerial, NoLotNumber(), serialNo, MustInventoryQuantity(1), actor, now)
	if err != nil {
		t.Fatalf("NewInventory(SERIAL) = %v", err)
	}
	inv.PullEvents()
	return inv
}

func hasEvent(inv *Inventory, name EventName) bool {
	for _, e := range inv.PullEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}

func onlyEvent(t *testing.T, inv *Inventory) Event {
	t.Helper()
	events := inv.PullEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	return events[0]
}

func qty(t *testing.T, v int64) Quantity {
	t.Helper()
	q, err := NewQuantity(v)
	if err != nil {
		t.Fatalf("NewQuantity(%d) = %v", v, err)
	}
	return q
}

// ---------------------------------------------------------------------------
// Factory
// ---------------------------------------------------------------------------

func TestNewInventoryNoneStartsActive(t *testing.T) {
	actor, now := act()
	inv, err := NewInventory(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		TrackingNone, NoLotNumber(), NoSerialNumber(), MustInventoryQuantity(10), actor, now)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status() != StatusActive {
		t.Errorf("status = %q, want ACTIVE", inv.Status())
	}
	if inv.OnHand().Value() != 10 || inv.Reserved().Value() != 0 || inv.Available().Value() != 10 {
		t.Errorf("counts wrong: on=%d res=%d avl=%d", inv.OnHand().Value(), inv.Reserved().Value(), inv.Available().Value())
	}
	if inv.Version() != 1 {
		t.Errorf("version = %d, want 1", inv.Version())
	}
	ev := onlyEvent(t, inv)
	if ev.Name != EventInventoryCreated {
		t.Fatalf("event = %q, want created", ev.Name)
	}
	for _, key := range []string{"product_id", "location_id", "warehouse_id", "tracking", "status", "on_hand"} {
		if _, ok := ev.Attributes[key]; !ok {
			t.Errorf("creation event missing %q", key)
		}
	}
}

func TestNewInventoryRejectsNilIdentifiers(t *testing.T) {
	actor, now := act()
	good := func() (a, b, c, d, e uuid.UUID) { return uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New() }
	id, co, wh, lo, pr := good()
	cases := map[string][5]uuid.UUID{
		"nil id":        {uuid.Nil, co, wh, lo, pr},
		"nil company":   {id, uuid.Nil, wh, lo, pr},
		"nil warehouse": {id, co, uuid.Nil, lo, pr},
		"nil location":  {id, co, wh, uuid.Nil, pr},
		"nil product":   {id, co, wh, lo, uuid.Nil},
	}
	for label, ids := range cases {
		_, err := NewInventory(ids[0], ids[1], ids[2], ids[3], ids[4],
			TrackingNone, NoLotNumber(), NoSerialNumber(), MustInventoryQuantity(1), actor, now)
		if err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
	// nil actor
	if _, err := NewInventory(id, co, wh, lo, pr, TrackingNone, NoLotNumber(), NoSerialNumber(),
		MustInventoryQuantity(1), uuid.Nil, now); err == nil {
		t.Error("nil actor: expected rejection")
	}
}

func TestNewInventoryTrackingPresenceRules(t *testing.T) {
	actor, now := act()
	lot, _ := NewLotNumber("L1")
	serial, _ := NewSerialNumber("S1")
	base := func(tr TrackingType, l LotNumber, s SerialNumber, q int64) error {
		_, err := NewInventory(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
			tr, l, s, MustInventoryQuantity(q), actor, now)
		return err
	}

	// NONE must carry neither.
	if base(TrackingNone, lot, NoSerialNumber(), 1) == nil {
		t.Error("NONE with a lot was accepted")
	}
	if base(TrackingNone, NoLotNumber(), serial, 1) == nil {
		t.Error("NONE with a serial was accepted")
	}
	// LOT requires a lot, forbids a serial.
	if base(TrackingLot, NoLotNumber(), NoSerialNumber(), 5) == nil {
		t.Error("LOT without a lot was accepted")
	}
	if base(TrackingLot, lot, serial, 5) == nil {
		t.Error("LOT with a serial was accepted")
	}
	// SERIAL requires a serial, forbids a lot, requires quantity one.
	if base(TrackingSerial, NoLotNumber(), NoSerialNumber(), 1) == nil {
		t.Error("SERIAL without a serial was accepted")
	}
	if base(TrackingSerial, lot, serial, 1) == nil {
		t.Error("SERIAL with a lot was accepted")
	}
	if base(TrackingSerial, NoLotNumber(), serial, 2) == nil {
		t.Error("SERIAL with quantity two was accepted")
	}
	if base(TrackingSerial, NoLotNumber(), serial, 1) != nil {
		t.Error("a valid SERIAL was rejected")
	}
	if base("BOGUS", NoLotNumber(), NoSerialNumber(), 1) == nil {
		t.Error("an invalid tracking type was accepted")
	}
}

// ---------------------------------------------------------------------------
// Reconstitution
// ---------------------------------------------------------------------------

func TestReconstituteRaisesNoEvents(t *testing.T) {
	actor, now := act()
	onHand := MustInventoryQuantity(5)
	reserved, _ := NewReservedQuantity(2)
	inv, err := Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		TrackingNone, StatusActive, onHand, reserved, NoLotNumber(), NoSerialNumber(),
		7, actor, actor, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	if inv.Version() != 7 {
		t.Errorf("version = %d, want restored 7", inv.Version())
	}
	if inv.Available().Value() != 3 {
		t.Errorf("available = %d, want 3", inv.Available().Value())
	}
}

func TestReconstituteRejectsInvalidState(t *testing.T) {
	actor, now := act()
	on5 := MustInventoryQuantity(5)
	res2, _ := NewReservedQuantity(2)
	res9, _ := NewReservedQuantity(9)
	on1 := MustInventoryQuantity(1)
	serial, _ := NewSerialNumber("S1")
	res0, _ := NewReservedQuantity(0)

	cases := map[string]func() (*Inventory, error){
		"version zero": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingNone, StatusActive, on5, res2, NoLotNumber(), NoSerialNumber(), 0, actor, actor, now, now)
		},
		"invalid status": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingNone, InventoryStatus("X"), on5, res2, NoLotNumber(), NoSerialNumber(), 1, actor, actor, now, now)
		},
		"reserved exceeds on-hand": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingNone, StatusActive, on5, res9, NoLotNumber(), NoSerialNumber(), 1, actor, actor, now, now)
		},
		"none carrying a serial": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingNone, StatusActive, on5, res2, NoLotNumber(), serial, 1, actor, actor, now, now)
		},
		"serial with on-hand not one": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingSerial, StatusActive, on5, res0, NoLotNumber(), serial, 1, actor, actor, now, now)
		},
		"serial valid": func() (*Inventory, error) {
			return Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), TrackingSerial, StatusActive, on1, res0, NoLotNumber(), serial, 1, actor, actor, now, now)
		},
	}
	for label, fn := range cases {
		_, err := fn()
		if label == "serial valid" {
			if err != nil {
				t.Errorf("%s: unexpected rejection %v", label, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

// ---------------------------------------------------------------------------
// Increase / Decrease
// ---------------------------------------------------------------------------

func TestIncrease(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.Increase(qty(t, 5), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 15 {
		t.Errorf("on-hand = %d, want 15", inv.OnHand().Value())
	}
	if ev := onlyEvent(t, inv); ev.Name != EventInventoryIncreased || ev.Attributes["amount"] != int64(5) || ev.Attributes["on_hand"] != int64(15) {
		t.Errorf("increased event wrong: %+v", ev.Attributes)
	}
}

func TestDecrease(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.Decrease(qty(t, 4), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 6 {
		t.Errorf("on-hand = %d, want 6", inv.OnHand().Value())
	}
	if !hasEvent(inv, EventInventoryDecreased) {
		t.Error("no decreased event")
	}
}

func TestDecreaseCannotGoNegative(t *testing.T) {
	inv := newNone(t, 3)
	actor, now := act()
	if err := inv.Decrease(qty(t, 5), actor, now); err == nil {
		t.Fatal("decreasing below zero was accepted")
	}
	if inv.OnHand().Value() != 3 {
		t.Error("a failed decrease mutated on-hand")
	}
	if got := inv.PullEvents(); len(got) != 0 {
		t.Error("a failed decrease emitted an event")
	}
}

func TestDecreaseCannotDropBelowReserved(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.Reserve(qty(t, 6), actor, now); err != nil {
		t.Fatal(err)
	}
	inv.PullEvents()
	// on-hand 10, reserved 6 -> cannot decrease by more than 4.
	if err := inv.Decrease(qty(t, 5), actor, now); err == nil {
		t.Fatal("decreasing below the reserved quantity was accepted")
	}
	if inv.OnHand().Value() != 10 {
		t.Error("a rejected decrease mutated on-hand")
	}
	// Exactly 4 is allowed.
	if err := inv.Decrease(qty(t, 4), actor, now); err != nil {
		t.Fatalf("decreasing to the reserved floor = %v", err)
	}
	if inv.OnHand().Value() != 6 || inv.Available().Value() != 0 {
		t.Errorf("counts after floor decrease: on=%d avl=%d", inv.OnHand().Value(), inv.Available().Value())
	}
}

// ---------------------------------------------------------------------------
// Reserve / Release
// ---------------------------------------------------------------------------

func TestReserveAndAvailability(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.Reserve(qty(t, 7), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.Reserved().Value() != 7 || inv.Available().Value() != 3 {
		t.Errorf("res=%d avl=%d, want 7/3", inv.Reserved().Value(), inv.Available().Value())
	}
	if !hasEvent(inv, EventInventoryReserved) {
		t.Error("no reserved event")
	}
}

func TestReserveCannotExceedAvailable(t *testing.T) {
	inv := newNone(t, 5)
	actor, now := act()
	if err := inv.Reserve(qty(t, 6), actor, now); err == nil {
		t.Fatal("reserving more than available was accepted")
	}
	if inv.Reserved().Value() != 0 {
		t.Error("a rejected reserve mutated reserved")
	}
}

func TestReleaseReservation(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 8), actor, now)
	inv.PullEvents()
	if err := inv.ReleaseReservation(qty(t, 3), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.Reserved().Value() != 5 || inv.Available().Value() != 5 {
		t.Errorf("res=%d avl=%d, want 5/5", inv.Reserved().Value(), inv.Available().Value())
	}
	if !hasEvent(inv, EventInventoryReleased) {
		t.Error("no released event")
	}
}

func TestReleaseCannotExceedReserved(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 2), actor, now)
	inv.PullEvents()
	if err := inv.ReleaseReservation(qty(t, 3), actor, now); err == nil {
		t.Fatal("releasing more than reserved was accepted")
	}
	if inv.Reserved().Value() != 2 {
		t.Error("a rejected release mutated reserved")
	}
}

// ---------------------------------------------------------------------------
// Adjust / CycleCount
// ---------------------------------------------------------------------------

func TestAdjustSetsAbsoluteValue(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.Adjust(MustInventoryQuantity(8), "damage", actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 8 {
		t.Errorf("on-hand = %d, want 8", inv.OnHand().Value())
	}
	ev := onlyEvent(t, inv)
	if ev.Name != EventInventoryAdjusted || ev.Attributes["previous_on_hand"] != int64(10) || ev.Attributes["reason"] != "damage" {
		t.Errorf("adjusted event wrong: %+v", ev.Attributes)
	}
}

func TestAdjustCannotDropBelowReserved(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 6), actor, now)
	inv.PullEvents()
	if err := inv.Adjust(MustInventoryQuantity(5), "recount", actor, now); err == nil {
		t.Fatal("adjusting below reserved was accepted")
	}
	if inv.OnHand().Value() != 10 {
		t.Error("a rejected adjust mutated on-hand")
	}
}

func TestCompleteCycleCountRecordsVariance(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.CompleteCycleCount(MustInventoryQuantity(7), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 7 {
		t.Errorf("on-hand = %d, want 7", inv.OnHand().Value())
	}
	ev := onlyEvent(t, inv)
	if ev.Name != EventCycleCountCompleted || ev.Attributes["variance"] != int64(-3) || ev.Attributes["counted"] != int64(7) {
		t.Errorf("cycle count event wrong: %+v", ev.Attributes)
	}
}

func TestCycleCountCannotDropBelowReserved(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 8), actor, now)
	inv.PullEvents()
	if err := inv.CompleteCycleCount(MustInventoryQuantity(5), actor, now); err == nil {
		t.Fatal("counting below reserved was accepted")
	}
}

// ---------------------------------------------------------------------------
// Transfers
// ---------------------------------------------------------------------------

func TestTransferOutMovesAvailableOnly(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 7), actor, now) // available now 3
	inv.PullEvents()

	if err := inv.TransferOut(qty(t, 4), actor, now); err == nil {
		t.Fatal("transferring more than available was accepted")
	}
	if err := inv.TransferOut(qty(t, 3), actor, now); err != nil {
		t.Fatalf("TransferOut(3) = %v", err)
	}
	if inv.OnHand().Value() != 7 {
		t.Errorf("on-hand = %d, want 7", inv.OnHand().Value())
	}
	ev := onlyEvent(t, inv)
	if ev.Name != EventInventoryTransferred || ev.Attributes["direction"] != TransferDirectionOut {
		t.Errorf("transfer-out event wrong: %+v", ev.Attributes)
	}
}

func TestTransferIn(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	if err := inv.TransferIn(qty(t, 6), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 16 {
		t.Errorf("on-hand = %d, want 16", inv.OnHand().Value())
	}
	ev := onlyEvent(t, inv)
	if ev.Name != EventInventoryTransferred || ev.Attributes["direction"] != TransferDirectionIn {
		t.Errorf("transfer-in event wrong: %+v", ev.Attributes)
	}
}

// ---------------------------------------------------------------------------
// Lot tracking
// ---------------------------------------------------------------------------

func TestLotTrackedBehavesLikeQuantityPool(t *testing.T) {
	inv := newLot(t, "LOT-A", 100)
	actor, now := act()
	if !inv.HasLot() || inv.Lot().String() != "LOT-A" {
		t.Fatalf("lot not set: %q", inv.Lot())
	}
	if err := inv.Decrease(qty(t, 40), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := inv.Reserve(qty(t, 30), actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.OnHand().Value() != 60 || inv.Reserved().Value() != 30 || inv.Available().Value() != 30 {
		t.Errorf("lot counts: on=%d res=%d avl=%d", inv.OnHand().Value(), inv.Reserved().Value(), inv.Available().Value())
	}
}

// ---------------------------------------------------------------------------
// Serial tracking
// ---------------------------------------------------------------------------

func TestSerialQuantityIsFixedAtOne(t *testing.T) {
	actor, now := act()
	// Every quantity-changing behaviour must be refused on a serial record.
	for _, tc := range []struct {
		name string
		fn   func(inv *Inventory) error
	}{
		{"Increase", func(inv *Inventory) error { return inv.Increase(qty(t, 1), actor, now) }},
		{"Decrease", func(inv *Inventory) error { return inv.Decrease(qty(t, 1), actor, now) }},
		{"TransferOut", func(inv *Inventory) error { return inv.TransferOut(qty(t, 1), actor, now) }},
		{"TransferIn", func(inv *Inventory) error { return inv.TransferIn(qty(t, 1), actor, now) }},
		{"Adjust to 2", func(inv *Inventory) error { return inv.Adjust(MustInventoryQuantity(2), "x", actor, now) }},
		{"CycleCount to 0", func(inv *Inventory) error { return inv.CompleteCycleCount(MustInventoryQuantity(0), actor, now) }},
	} {
		inv := newSerial(t, "SN-1")
		if err := tc.fn(inv); err == nil {
			t.Errorf("%s on a serial record was accepted", tc.name)
		}
		if inv.OnHand().Value() != 1 {
			t.Errorf("%s changed serial on-hand to %d", tc.name, inv.OnHand().Value())
		}
	}
}

func TestSerialAllowsReserveReleaseAndConfirmCount(t *testing.T) {
	inv := newSerial(t, "SN-1")
	actor, now := act()

	if err := inv.Reserve(qty(t, 1), actor, now); err != nil {
		t.Fatalf("reserving the single serial unit = %v", err)
	}
	if inv.Available().Value() != 0 || inv.OnHand().Value() != 1 {
		t.Errorf("serial after reserve: on=%d avl=%d", inv.OnHand().Value(), inv.Available().Value())
	}
	if err := inv.ReleaseReservation(qty(t, 1), actor, now); err != nil {
		t.Fatalf("releasing the serial reservation = %v", err)
	}
	// A cycle count that confirms presence (counted == 1) is allowed.
	if err := inv.CompleteCycleCount(MustInventoryQuantity(1), actor, now); err != nil {
		t.Fatalf("confirming a serial by count = %v", err)
	}
	if inv.OnHand().Value() != 1 {
		t.Errorf("serial on-hand drifted to %d", inv.OnHand().Value())
	}
}

// ---------------------------------------------------------------------------
// State machine: ACTIVE <-> LOCKED
// ---------------------------------------------------------------------------

func TestLockUnlockCycle(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()

	if err := inv.Lock(actor, now); err != nil {
		t.Fatal(err)
	}
	if !inv.IsLocked() {
		t.Error("Lock did not lock")
	}
	if ev := onlyEvent(t, inv); ev.Name != EventInventoryLocked {
		t.Errorf("event = %q, want locked", ev.Name)
	}
	// Locking again is a conflict.
	if err := inv.Lock(actor, now); err == nil {
		t.Error("locking an already-locked record was accepted")
	}

	if err := inv.Unlock(actor, now); err != nil {
		t.Fatal(err)
	}
	if inv.IsLocked() {
		t.Error("Unlock did not unlock")
	}
	if ev := onlyEvent(t, inv); ev.Name != EventInventoryUnlocked {
		t.Errorf("event = %q, want unlocked", ev.Name)
	}
	// Unlocking an active record is a conflict.
	if err := inv.Unlock(actor, now); err == nil {
		t.Error("unlocking an active record was accepted")
	}
}

func TestLockedRecordRefusesEveryMovement(t *testing.T) {
	actor, now := act()
	for _, tc := range []struct {
		name string
		fn   func(inv *Inventory) error
	}{
		{"Increase", func(inv *Inventory) error { return inv.Increase(qty(t, 1), actor, now) }},
		{"Decrease", func(inv *Inventory) error { return inv.Decrease(qty(t, 1), actor, now) }},
		{"Reserve", func(inv *Inventory) error { return inv.Reserve(qty(t, 1), actor, now) }},
		{"Release", func(inv *Inventory) error { return inv.ReleaseReservation(qty(t, 1), actor, now) }},
		{"Adjust", func(inv *Inventory) error { return inv.Adjust(MustInventoryQuantity(5), "x", actor, now) }},
		{"TransferOut", func(inv *Inventory) error { return inv.TransferOut(qty(t, 1), actor, now) }},
		{"TransferIn", func(inv *Inventory) error { return inv.TransferIn(qty(t, 1), actor, now) }},
		{"CycleCount", func(inv *Inventory) error { return inv.CompleteCycleCount(MustInventoryQuantity(5), actor, now) }},
	} {
		inv := newNone(t, 10)
		_ = inv.Lock(actor, now)
		inv.PullEvents()
		if err := tc.fn(inv); err == nil {
			t.Errorf("%s on a locked record was accepted", tc.name)
		}
		if got := inv.PullEvents(); len(got) != 0 {
			t.Errorf("%s on a locked record emitted an event", tc.name)
		}
	}
}

// ---------------------------------------------------------------------------
// Movement amounts and optimistic locking
// ---------------------------------------------------------------------------

func TestZeroOrNegativeAmountsAreRejected(t *testing.T) {
	for _, v := range []int64{0, -1, -100} {
		if _, err := NewQuantity(v); err == nil {
			t.Errorf("NewQuantity(%d) was accepted", v)
		}
	}
}

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	inv := newNone(t, 100)
	actor, now := act()
	start := inv.Version()

	_ = inv.Increase(qty(t, 10), actor, now)
	_ = inv.Decrease(qty(t, 5), actor, now)
	_ = inv.Reserve(qty(t, 20), actor, now)
	_ = inv.ReleaseReservation(qty(t, 5), actor, now)
	_ = inv.Adjust(MustInventoryQuantity(90), "x", actor, now)
	_ = inv.TransferOut(qty(t, 10), actor, now)
	_ = inv.TransferIn(qty(t, 10), actor, now)
	_ = inv.CompleteCycleCount(MustInventoryQuantity(80), actor, now)
	_ = inv.Lock(actor, now)
	_ = inv.Unlock(actor, now)

	if inv.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d — the repository owns it", inv.Version(), start)
	}
}

func TestOnHandNeverBelowReservedInvariantHolds(t *testing.T) {
	inv := newNone(t, 10)
	actor, now := act()
	_ = inv.Reserve(qty(t, 10), actor, now) // reserve everything
	inv.PullEvents()
	// No behaviour may leave on-hand below reserved.
	_ = inv.Decrease(qty(t, 1), actor, now)                          // must fail
	_ = inv.Adjust(MustInventoryQuantity(9), "x", actor, now)        // must fail
	_ = inv.CompleteCycleCount(MustInventoryQuantity(9), actor, now) // must fail
	_ = inv.TransferOut(qty(t, 1), actor, now)                       // must fail (available 0)
	if inv.OnHand().Value() < inv.Reserved().Value() {
		t.Fatal("the OnHand >= Reserved invariant was violated")
	}
	if inv.OnHand().Value() != 10 || inv.Reserved().Value() != 10 {
		t.Errorf("counts drifted: on=%d res=%d", inv.OnHand().Value(), inv.Reserved().Value())
	}
}
