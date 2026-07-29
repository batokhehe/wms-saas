package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func act() (uuid.UUID, time.Time) { return uuid.New(), time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC) }

func untrackedKey(t *testing.T) StockKey {
	t.Helper()
	key, err := NewStockKey(uuid.New(), uuid.New(), uuid.New(), uuid.New(), UntrackedAttributes())
	if err != nil {
		t.Fatalf("NewStockKey() = %v", err)
	}
	return key
}

func serialKey(t *testing.T, serial string) StockKey {
	t.Helper()
	sn, err := NewSerialNumber(serial)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := NewStockAttributes(TrackingSerial, NoLotNumber(), sn)
	if err != nil {
		t.Fatal(err)
	}
	key, err := NewStockKey(uuid.New(), uuid.New(), uuid.New(), uuid.New(), attrs)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// newPosition opens a position holding onHand units, all available.
func newPosition(t *testing.T, onHand int64) *InventoryPosition {
	t.Helper()
	actor, now := act()
	p, err := NewInventoryPosition(uuid.New(), untrackedKey(t), actor, now)
	if err != nil {
		t.Fatalf("NewInventoryPosition() = %v", err)
	}
	if onHand > 0 {
		if err := p.Receive(MustQuantity(onHand), actor, now); err != nil {
			t.Fatalf("Receive() = %v", err)
		}
	}
	p.PullEvents()
	return p
}

func hasEvent(p *InventoryPosition, name EventName) bool {
	for _, e := range p.PullEvents() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// assertBuckets checks all four balances and the derived total together, so a
// transition that moves stock into the wrong bucket cannot pass.
func assertBuckets(t *testing.T, p *InventoryPosition, available, reserved, allocated, quarantined int64) {
	t.Helper()
	if p.Available().Value() != available || p.Reserved().Value() != reserved ||
		p.Allocated().Value() != allocated || p.Quarantined().Value() != quarantined {
		t.Fatalf("buckets = avail:%d res:%d alloc:%d quar:%d; want %d/%d/%d/%d",
			p.Available().Value(), p.Reserved().Value(), p.Allocated().Value(), p.Quarantined().Value(),
			available, reserved, allocated, quarantined)
	}
	want := available + reserved + allocated + quarantined
	if p.OnHand() != want {
		t.Fatalf("OnHand() = %d, want %d (derived from the buckets)", p.OnHand(), want)
	}
}

// ---------- Factory ----------

func TestNewPositionOpensEmpty(t *testing.T) {
	actor, now := act()
	p, err := NewInventoryPosition(uuid.New(), untrackedKey(t), actor, now)
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 0, 0, 0, 0)
	if !p.IsEmpty() || p.Version() != 1 {
		t.Error("a new position should be empty at version 1")
	}
	events := p.PullEvents()
	if len(events) != 1 || events[0].Name != EventPositionCreated {
		t.Fatalf("expected one created event, got %+v", events)
	}
}

func TestNewPositionRejectsNilIdentifiers(t *testing.T) {
	actor, now := act()
	key := untrackedKey(t)
	if _, err := NewInventoryPosition(uuid.Nil, key, actor, now); err == nil {
		t.Error("nil position id accepted")
	}
	if _, err := NewInventoryPosition(uuid.New(), key, uuid.Nil, now); err == nil {
		t.Error("nil actor accepted")
	}
	if _, err := NewInventoryPosition(uuid.New(), StockKey{}, actor, now); err == nil {
		t.Error("empty stock key accepted")
	}
}

// ---------- Receive / Issue ----------

func TestReceiveLandsInAvailable(t *testing.T) {
	p := newPosition(t, 0)
	actor, now := act()
	if err := p.Receive(MustQuantity(40), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 40, 0, 0, 0)
	if !hasEvent(p, EventStockReceived) {
		t.Error("no received event")
	}
}

func TestIssueTakesFromAvailable(t *testing.T) {
	p := newPosition(t, 40)
	actor, now := act()
	if err := p.Issue(MustQuantity(15), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 25, 0, 0, 0)
	if !hasEvent(p, EventStockIssued) {
		t.Error("no issued event")
	}
}

func TestIssueBeyondAvailableIsRefused(t *testing.T) {
	p := newPosition(t, 10)
	actor, now := act()
	if err := p.Issue(MustQuantity(11), actor, now); err == nil {
		t.Fatal("issuing more than available was accepted")
	}
	assertBuckets(t, p, 10, 0, 0, 0)
	if got := p.PullEvents(); len(got) != 0 {
		t.Error("a refused issue emitted an event")
	}
}

// TestIssueCannotTakeEncumberedStock is the bucket model's central guarantee:
// stock that is promised, assigned or held is unreachable by an ordinary issue.
func TestIssueCannotTakeEncumberedStock(t *testing.T) {
	actor, now := act()
	p := newPosition(t, 30)
	if err := p.Reserve(MustQuantity(10), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Reserve(MustQuantity(10), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Allocate(MustQuantity(10), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.MoveToQuarantine(MustQuantity(5), actor, now); err != nil {
		t.Fatal(err)
	}
	p.PullEvents()
	// avail 5, reserved 10, allocated 10, quarantined 5 — on-hand still 30.
	assertBuckets(t, p, 5, 10, 10, 5)

	if err := p.Issue(MustQuantity(6), actor, now); err == nil {
		t.Fatal("an issue reached past available into encumbered stock")
	}
	if err := p.Issue(MustQuantity(5), actor, now); err != nil {
		t.Fatalf("issuing exactly the available balance = %v", err)
	}
	assertBuckets(t, p, 0, 10, 10, 5)
}

// ---------- Reserve / Release ----------

func TestReserveAndRelease(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()

	if err := p.Reserve(MustQuantity(12), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 8, 12, 0, 0)
	if !hasEvent(p, EventStockReserved) {
		t.Error("no reserved event")
	}

	if err := p.Release(MustQuantity(5), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 13, 7, 0, 0)
	if !hasEvent(p, EventStockReleased) {
		t.Error("no released event")
	}
}

func TestReserveBeyondAvailableAndReleaseBeyondReserved(t *testing.T) {
	p := newPosition(t, 5)
	actor, now := act()

	if err := p.Reserve(MustQuantity(6), actor, now); err == nil {
		t.Error("reserving more than available was accepted")
	}
	if err := p.Reserve(MustQuantity(5), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Release(MustQuantity(6), actor, now); err == nil {
		t.Error("releasing more than reserved was accepted")
	}
	assertBuckets(t, p, 0, 5, 0, 0)
}

// ---------- Allocate / Deallocate ----------

// TestAllocateDrawsFromReservedNotAvailable pins the two-stage commitment:
// allocation hardens an existing reservation, it does not create one.
func TestAllocateDrawsFromReservedNotAvailable(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()

	// Nothing reserved yet, so there is nothing to harden — even though 20 units
	// are sitting available.
	if err := p.Allocate(MustQuantity(1), actor, now); err == nil {
		t.Fatal("allocated straight from available, bypassing the reservation stage")
	}
	assertBuckets(t, p, 20, 0, 0, 0)

	if err := p.Reserve(MustQuantity(12), actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Allocate(MustQuantity(9), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 8, 3, 9, 0)
	if !hasEvent(p, EventStockAllocated) {
		t.Error("no allocated event")
	}
}

func TestDeallocateReturnsToReserved(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()
	_ = p.Reserve(MustQuantity(10), actor, now)
	_ = p.Allocate(MustQuantity(10), actor, now)
	p.PullEvents()

	if err := p.Deallocate(MustQuantity(4), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 10, 4, 6, 0)
	if !hasEvent(p, EventStockDeallocated) {
		t.Error("no deallocated event")
	}
	if err := p.Deallocate(MustQuantity(7), actor, now); err == nil {
		t.Error("deallocating more than allocated was accepted")
	}
}

// ---------- Quarantine ----------

func TestQuarantineIsPartialAndReversible(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()

	if err := p.MoveToQuarantine(MustQuantity(6), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 14, 0, 0, 6)
	if !p.IsQuarantined() {
		t.Error("IsQuarantined() = false with stock held")
	}
	if !hasEvent(p, EventStockQuarantined) {
		t.Error("no quarantined event")
	}

	if err := p.ReleaseFromQuarantine(MustQuantity(6), actor, now); err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, p, 20, 0, 0, 0)
	if p.IsQuarantined() {
		t.Error("IsQuarantined() = true after full release")
	}
	if !hasEvent(p, EventStockReleasedFromQuarantine) {
		t.Error("no released-from-quarantine event")
	}
}

func TestQuarantineBeyondAvailableIsRefused(t *testing.T) {
	p := newPosition(t, 5)
	actor, now := act()
	if err := p.MoveToQuarantine(MustQuantity(6), actor, now); err == nil {
		t.Error("quarantining more than available was accepted")
	}
	if err := p.ReleaseFromQuarantine(MustQuantity(1), actor, now); err == nil {
		t.Error("releasing from an empty quarantine bucket was accepted")
	}
}

// ---------- Adjust ----------

func TestAdjustAbsorbsVarianceInAvailable(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()
	_ = p.Reserve(MustQuantity(5), actor, now)
	p.PullEvents()

	if err := p.Adjust(18, "cycle count", actor, now); err != nil {
		t.Fatal(err)
	}
	// Reserved is untouched; only available moves to make the total 18.
	assertBuckets(t, p, 13, 5, 0, 0)
	if !hasEvent(p, EventStockAdjusted) {
		t.Error("no adjusted event")
	}
}

func TestAdjustBelowEncumberedIsRefused(t *testing.T) {
	p := newPosition(t, 20)
	actor, now := act()
	_ = p.Reserve(MustQuantity(8), actor, now)
	_ = p.Allocate(MustQuantity(4), actor, now)
	_ = p.MoveToQuarantine(MustQuantity(3), actor, now)
	p.PullEvents()
	// encumbered = reserved 4 + allocated 4 + quarantined 3 = 11
	if err := p.Adjust(10, "recount", actor, now); err == nil {
		t.Fatal("a count below the encumbered stock was accepted")
	}
	if err := p.Adjust(11, "recount", actor, now); err != nil {
		t.Fatalf("a count exactly at the encumbered floor = %v", err)
	}
	assertBuckets(t, p, 0, 4, 4, 3)
}

func TestAdjustRejectsNegative(t *testing.T) {
	p := newPosition(t, 5)
	actor, now := act()
	if err := p.Adjust(-1, "bad", actor, now); err == nil {
		t.Error("a negative count was accepted")
	}
}

// ---------- Serial ----------

func TestSerialPositionCannotExceedOneUnit(t *testing.T) {
	actor, now := act()
	p, err := NewInventoryPosition(uuid.New(), serialKey(t, "SN-1"), actor, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Receive(MustQuantity(1), actor, now); err != nil {
		t.Fatalf("receiving the single unit = %v", err)
	}
	if err := p.Receive(MustQuantity(1), actor, now); err == nil {
		t.Fatal("a serial position accepted a second unit")
	}
	if err := p.Adjust(2, "recount", actor, now); err == nil {
		t.Fatal("a serial position accepted an adjustment to two units")
	}
	if p.OnHand() != 1 {
		t.Errorf("on-hand = %d, want 1", p.OnHand())
	}
	// The encumbrance pipeline still works for a serial.
	if err := p.Reserve(MustQuantity(1), actor, now); err != nil {
		t.Fatalf("reserving a serial = %v", err)
	}
	if err := p.Allocate(MustQuantity(1), actor, now); err != nil {
		t.Fatalf("allocating a serial = %v", err)
	}
	assertBuckets(t, p, 0, 0, 1, 0)
}

// ---------- Reconstitution / version ----------

func TestReconstituteRaisesNoEventsAndRestoresBuckets(t *testing.T) {
	actor, now := act()
	p, err := Reconstitute(uuid.New(), untrackedKey(t),
		MustQuantityBucket(4), MustQuantityBucket(3), MustQuantityBucket(2), MustQuantityBucket(1),
		7, actor, actor, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
	assertBuckets(t, p, 4, 3, 2, 1)
	if p.Version() != 7 {
		t.Errorf("version = %d, want 7", p.Version())
	}
}

func TestReconstituteRejectsInvalidState(t *testing.T) {
	actor, now := act()
	key := untrackedKey(t)
	if _, err := Reconstitute(uuid.New(), key, EmptyBucket(), EmptyBucket(), EmptyBucket(), EmptyBucket(),
		0, actor, actor, now, now); err == nil {
		t.Error("version zero accepted")
	}
	// A serial position holding two units is corrupt.
	if _, err := Reconstitute(uuid.New(), serialKey(t, "SN-9"),
		MustQuantityBucket(2), EmptyBucket(), EmptyBucket(), EmptyBucket(),
		1, actor, actor, now, now); err == nil {
		t.Error("a serial position with two units was accepted")
	}
}

func TestVersionIsNeverMutatedByBehaviours(t *testing.T) {
	p := newPosition(t, 50)
	actor, now := act()
	start := p.Version()

	_ = p.Receive(MustQuantity(5), actor, now)
	_ = p.Reserve(MustQuantity(10), actor, now)
	_ = p.Allocate(MustQuantity(5), actor, now)
	_ = p.Deallocate(MustQuantity(2), actor, now)
	_ = p.Release(MustQuantity(3), actor, now)
	_ = p.MoveToQuarantine(MustQuantity(4), actor, now)
	_ = p.ReleaseFromQuarantine(MustQuantity(4), actor, now)
	_ = p.Issue(MustQuantity(6), actor, now)
	_ = p.Adjust(40, "count", actor, now)

	if p.Version() != start {
		t.Fatalf("a behaviour changed Version: got %d, want %d — the repository owns it", p.Version(), start)
	}
}

// TestBucketMovesConserveStock is the property that makes the model trustworthy:
// no bucket-to-bucket transition may change the total.
func TestBucketMovesConserveStock(t *testing.T) {
	p := newPosition(t, 100)
	actor, now := act()
	before := p.OnHand()

	_ = p.Reserve(MustQuantity(40), actor, now)
	_ = p.Allocate(MustQuantity(25), actor, now)
	_ = p.Deallocate(MustQuantity(10), actor, now)
	_ = p.Release(MustQuantity(5), actor, now)
	_ = p.MoveToQuarantine(MustQuantity(20), actor, now)
	_ = p.ReleaseFromQuarantine(MustQuantity(7), actor, now)

	if p.OnHand() != before {
		t.Fatalf("on-hand = %d after bucket moves, want %d — a move created or destroyed stock", p.OnHand(), before)
	}
}

// ---------- Value objects ----------

func TestQuantityAndBucketGuards(t *testing.T) {
	for _, v := range []int64{0, -1} {
		if _, err := NewQuantity(v); err == nil {
			t.Errorf("NewQuantity(%d) accepted", v)
		}
	}
	if _, err := NewQuantityBucket(-1); err == nil {
		t.Error("negative bucket accepted")
	}
	if _, err := MustQuantityBucket(9223372036854775806).Add(MustQuantity(10)); err == nil {
		t.Error("bucket overflow was not guarded")
	}
	if _, err := MustQuantityBucket(3).Sub(MustQuantity(4)); err == nil {
		t.Error("bucket underflow was not guarded")
	}
}

func TestStockAttributesEnforceTrackingTriple(t *testing.T) {
	lot, _ := NewLotNumber("L1")
	serial, _ := NewSerialNumber("S1")

	if _, err := NewStockAttributes(TrackingNone, lot, NoSerialNumber()); err == nil {
		t.Error("NONE with a lot accepted")
	}
	if _, err := NewStockAttributes(TrackingLot, NoLotNumber(), NoSerialNumber()); err == nil {
		t.Error("LOT without a lot accepted")
	}
	if _, err := NewStockAttributes(TrackingSerial, lot, serial); err == nil {
		t.Error("SERIAL with a lot accepted")
	}
	if _, err := NewStockAttributes(TrackingType("X"), NoLotNumber(), NoSerialNumber()); err == nil {
		t.Error("unknown tracking type accepted")
	}
	if _, err := NewStockAttributes(TrackingLot, lot, NoSerialNumber()); err != nil {
		t.Errorf("valid LOT attributes rejected: %v", err)
	}
}

func TestStockKeyEqualityAndRelocation(t *testing.T) {
	company, warehouse, location, product := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	key, err := NewStockKey(company, warehouse, location, product, UntrackedAttributes())
	if err != nil {
		t.Fatal(err)
	}
	same, _ := NewStockKey(company, warehouse, location, product, UntrackedAttributes())
	if !key.Equals(same) {
		t.Error("identical keys reported unequal")
	}

	moved, err := key.WithLocation(warehouse, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if key.Equals(moved) {
		t.Error("a relocated key reported equal to its origin")
	}
	if moved.ProductID() != product || !moved.Attributes().Equals(key.Attributes()) {
		t.Error("relocation changed the product or attributes")
	}

	if _, err := NewStockKey(uuid.Nil, warehouse, location, product, UntrackedAttributes()); err == nil {
		t.Error("nil company accepted in a stock key")
	}
}
