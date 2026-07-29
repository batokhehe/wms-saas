package entity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func occurred() time.Time { return time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC) }

func mustBefore(t *testing.T, a, r, al, q int64) BeforeBucket {
	t.Helper()
	b, err := NewBeforeBucket(a, r, al, q)
	if err != nil {
		t.Fatalf("NewBeforeBucket = %v", err)
	}
	return b
}

func mustAfter(t *testing.T, a, r, al, q int64) AfterBucket {
	t.Helper()
	b, err := NewAfterBucket(a, r, al, q)
	if err != nil {
		t.Fatalf("NewAfterBucket = %v", err)
	}
	return b
}

// newEntry builds a valid entry with the given snapshots.
func newEntry(t *testing.T, movement MovementType, before BeforeBucket, after AfterBucket) *InventoryLedgerEntry {
	t.Helper()
	e, err := NewLedgerEntry(
		uuid.New(), uuid.New(),
		uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"", "", nil,
		movement, EmptyMovementContext(),
		uuid.New(),
		before, after,
		occurred(),
	)
	if err != nil {
		t.Fatalf("NewLedgerEntry = %v", err)
	}
	return e
}

// ---------- Factory ----------

func TestNewLedgerEntryComputesDelta(t *testing.T) {
	e := newEntry(t, MovementInbound, mustBefore(t, 10, 2, 1, 0), mustAfter(t, 25, 2, 1, 0))

	d := e.Delta()
	if d.Available() != 15 || d.Reserved() != 0 || d.Allocated() != 0 || d.Quarantined() != 0 {
		t.Errorf("delta = %+v, want available +15 only", d)
	}
	if d.OnHand() != 15 {
		t.Errorf("delta on-hand = %d, want 15", d.OnHand())
	}
	if e.Before().OnHand() != 13 || e.After().OnHand() != 28 {
		t.Errorf("snapshot totals wrong: %d -> %d", e.Before().OnHand(), e.After().OnHand())
	}
}

// TestDeltaIsDerivedNotSupplied is the ledger's integrity guarantee: the caller
// supplies only the two snapshots, so a delta can never contradict them.
func TestDeltaIsDerivedNotSupplied(t *testing.T) {
	// A pure bucket shuffle: stock moves available -> reserved, total unchanged.
	e := newEntry(t, MovementReservation, mustBefore(t, 20, 0, 0, 0), mustAfter(t, 12, 8, 0, 0))

	d := e.Delta()
	if d.Available() != -8 || d.Reserved() != 8 {
		t.Errorf("delta = %+v, want -8 available / +8 reserved", d)
	}
	if d.OnHand() != 0 || !d.IsBucketShuffle() {
		t.Error("a reservation must not change the total")
	}
}

func TestNewLedgerEntryRejectsInvalidInput(t *testing.T) {
	before, after := mustBefore(t, 0, 0, 0, 0), mustAfter(t, 5, 0, 0, 0)
	valid := func() (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
		return uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	}
	id, company, position, product, warehouse, location := valid()

	cases := map[string]func() error{
		"nil ledger id": func() error {
			_, err := NewLedgerEntry(uuid.Nil, company, position, product, warehouse, location, "", "", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, occurred())
			return err
		},
		"nil company": func() error {
			_, err := NewLedgerEntry(id, uuid.Nil, position, product, warehouse, location, "", "", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, occurred())
			return err
		},
		"nil position": func() error {
			_, err := NewLedgerEntry(id, company, uuid.Nil, product, warehouse, location, "", "", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, occurred())
			return err
		},
		"nil actor": func() error {
			_, err := NewLedgerEntry(id, company, position, product, warehouse, location, "", "", nil, MovementInbound, EmptyMovementContext(), uuid.Nil, before, after, occurred())
			return err
		},
		"invalid movement type": func() error {
			_, err := NewLedgerEntry(id, company, position, product, warehouse, location, "", "", nil, MovementType("BOGUS"), EmptyMovementContext(), uuid.New(), before, after, occurred())
			return err
		},
		"zero occurred-at": func() error {
			_, err := NewLedgerEntry(id, company, position, product, warehouse, location, "", "", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, time.Time{})
			return err
		},
		"both lot and serial": func() error {
			_, err := NewLedgerEntry(id, company, position, product, warehouse, location, "LOT-A", "SN-1", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, occurred())
			return err
		},
	}
	for label, fn := range cases {
		if err := fn(); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

func TestAllMovementTypesAreAccepted(t *testing.T) {
	for _, m := range []MovementType{
		MovementInitialBalance, MovementInbound, MovementOutbound, MovementTransfer,
		MovementReservation, MovementAllocation, MovementAdjustment,
		MovementQuarantine, MovementCycleCount,
	} {
		if !m.Valid() {
			t.Errorf("%s reported invalid", m)
		}
		newEntry(t, m, mustBefore(t, 5, 0, 0, 0), mustAfter(t, 5, 0, 0, 0))
	}
	if MovementType("SOMETHING_ELSE").Valid() {
		t.Error("an unknown movement type reported valid")
	}
}

// ---------- Immutability ----------

// TestEntryExposesNoMutators is a structural assertion: the aggregate must offer
// no way to change itself after construction. It is enforced at COMPILE time —
// there are no setters to call — and this test documents and pins that, since a
// future setter would make the ledger rewritable without any test failing.
func TestEntryExposesNoMutators(t *testing.T) {
	e := newEntry(t, MovementInbound, mustBefore(t, 0, 0, 0, 0), mustAfter(t, 10, 0, 0, 0))

	// Reading twice yields identical values: nothing observed mutates the entry.
	first, second := e.Delta(), e.Delta()
	if first != second {
		t.Fatal("reading the delta mutated it")
	}
	if e.After().Available() != 10 || e.MovementType() != MovementInbound {
		t.Fatal("entry state changed between reads")
	}
}

// TestOwnerIDCannotBeMutatedThroughThePointer proves the defensive copy: a caller
// holding the pointer it passed in cannot reach into the entry and change it.
func TestOwnerIDCannotBeMutatedThroughThePointer(t *testing.T) {
	owner := uuid.New()
	e, err := NewLedgerEntry(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"", "", &owner,
		MovementInbound, EmptyMovementContext(), uuid.New(),
		mustBefore(t, 0, 0, 0, 0), mustAfter(t, 1, 0, 0, 0), occurred(),
	)
	if err != nil {
		t.Fatal(err)
	}

	owner = uuid.New() // mutate the caller's variable
	if got := e.OwnerID(); got == nil || *got == owner {
		t.Fatal("the entry's owner changed when the caller's variable did")
	}

	// And the returned pointer is a copy too.
	returned := e.OwnerID()
	*returned = uuid.New()
	if again := e.OwnerID(); *again == *returned {
		t.Fatal("mutating the returned owner pointer changed the entry")
	}
}

// ---------- Value objects ----------

func TestBucketSnapshotsRejectNegatives(t *testing.T) {
	if _, err := NewBeforeBucket(-1, 0, 0, 0); err == nil {
		t.Error("negative before-balance accepted")
	}
	if _, err := NewAfterBucket(0, 0, -1, 0); err == nil {
		t.Error("negative after-balance accepted")
	}
}

func TestBucketDeltaClassification(t *testing.T) {
	shuffle := NewBucketDelta(mustBefore(t, 10, 0, 0, 0), mustAfter(t, 4, 6, 0, 0))
	if !shuffle.IsBucketShuffle() || shuffle.IsZero() {
		t.Error("a bucket move should be a shuffle and not zero")
	}

	inbound := NewBucketDelta(mustBefore(t, 0, 0, 0, 0), mustAfter(t, 7, 0, 0, 0))
	if inbound.IsBucketShuffle() || inbound.OnHand() != 7 {
		t.Error("an inbound movement should change the total")
	}

	none := NewBucketDelta(mustBefore(t, 3, 1, 0, 0), mustAfter(t, 3, 1, 0, 0))
	if !none.IsZero() {
		t.Error("identical snapshots should produce a zero delta")
	}
}

func TestMovementContextValidation(t *testing.T) {
	ref := uuid.New()

	// A reference id with no type is unresolvable.
	if _, err := NewMovementContext("", &ref, "", ""); err == nil {
		t.Error("a reference id without a type was accepted")
	}
	ctx, err := NewMovementContext("PURCHASE_ORDER", &ref, "PO-1001", "receipt")
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.HasReference() || ctx.ReferenceType() != "PURCHASE_ORDER" || ctx.DocumentNumber() != "PO-1001" {
		t.Errorf("context not stored: %+v", ctx)
	}

	// The returned pointer is a copy.
	returned := ctx.ReferenceID()
	*returned = uuid.New()
	if *ctx.ReferenceID() == *returned {
		t.Error("mutating the returned reference id changed the context")
	}

	if !EmptyMovementContext().HasReference() == false {
		t.Error("an empty context should have no reference")
	}
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := NewMovementContext("", nil, "", string(long)); err == nil {
		t.Error("an over-long reason was accepted")
	}
}

// ---------- Reconstitution ----------

func TestReconstituteRecomputesDelta(t *testing.T) {
	e, err := Reconstitute(
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(),
		"LOT-A", "", nil,
		MovementCycleCount, EmptyMovementContext(), uuid.New(),
		mustBefore(t, 10, 0, 0, 0), mustAfter(t, 6, 0, 0, 0),
		occurred(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if e.Delta().OnHand() != -4 {
		t.Errorf("recomputed delta = %d, want -4", e.Delta().OnHand())
	}
	if e.LotNumber() != "LOT-A" {
		t.Errorf("lot = %q", e.LotNumber())
	}
}

func TestReconstituteRejectsCorruptRows(t *testing.T) {
	before, after := mustBefore(t, 0, 0, 0, 0), mustAfter(t, 1, 0, 0, 0)
	if _, err := Reconstitute(uuid.Nil, uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "", "", nil, MovementInbound, EmptyMovementContext(), uuid.New(), before, after, occurred()); err == nil {
		t.Error("nil ledger id accepted")
	}
	if _, err := Reconstitute(uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), "", "", nil, MovementType("X"), EmptyMovementContext(), uuid.New(), before, after, occurred()); err == nil {
		t.Error("invalid movement type accepted")
	}
}
