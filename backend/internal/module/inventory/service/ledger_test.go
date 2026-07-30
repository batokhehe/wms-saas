package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	ledgerdto "github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
)

// These tests pin the ledger integration's contract:
//
//	one movement  ->  exactly one entry
//	before + delta = after, for every bucket
//	a failed append rolls the movement back
//	replaying the entries reproduces the position
//
// They exercise the SERVICE, not the ledger — the ledger's own arithmetic is
// tested in its package. What is under test here is that the inventory service
// reports the right snapshots, exactly once, inside the transaction.

// onHand sums a snapshot's four balances.
func onHand(b ledgerdto.BucketSnapshotRequest) int64 {
	return b.Available + b.Reserved + b.Allocated + b.Quarantined
}

// assertSnapshotsAgree checks that a recorded movement's before and after
// snapshots differ by exactly the change the movement made.
func assertSnapshotsAgree(
	t *testing.T, movement ledgerdto.RecordMovementRequest,
	wantAvailable, wantReserved, wantAllocated, wantQuarantined int64,
) {
	t.Helper()
	deltas := map[string][2]int64{
		"available":   {movement.After.Available - movement.Before.Available, wantAvailable},
		"reserved":    {movement.After.Reserved - movement.Before.Reserved, wantReserved},
		"allocated":   {movement.After.Allocated - movement.Before.Allocated, wantAllocated},
		"quarantined": {movement.After.Quarantined - movement.Before.Quarantined, wantQuarantined},
	}
	for bucket, pair := range deltas {
		if pair[0] != pair[1] {
			t.Errorf("%s delta = %d, want %d", bucket, pair[0], pair[1])
		}
	}
}

// ---------- One entry per movement ----------

func TestReceiveAppendsExactlyOneInboundEntry(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	place := newPlace()

	got, err := h.svc.ReceiveStock(ctx, receiveReq(place, 25))
	if err != nil {
		t.Fatal(err)
	}

	movement := h.ledger.only(t)
	if movement.MovementType != movementInbound {
		t.Errorf("movement type = %q, want INBOUND", movement.MovementType)
	}
	if movement.PositionID != got.ID {
		t.Error("the entry does not name the position that moved")
	}
	if movement.WarehouseID != place.warehouse || movement.LocationID != place.location ||
		movement.ProductID != place.product {
		t.Error("the entry does not carry the position's coordinates")
	}
	if movement.ReferenceType != "RECEIVE" {
		t.Errorf("reference type = %q, want RECEIVE", movement.ReferenceType)
	}
	if onHand(movement.Before) != 0 || onHand(movement.After) != 25 {
		t.Errorf("snapshots = %d -> %d, want 0 -> 25", onHand(movement.Before), onHand(movement.After))
	}
	assertSnapshotsAgree(t, movement, 25, 0, 0, 0)
}

// TestReceiveUnderQuarantinePolicyStillAppendsOneEntry pins the corner case: the
// policy moves the units straight to quarantine, which is two bucket changes but
// still ONE arrival. Two entries would double-count it in any INBOUND report.
func TestReceiveUnderQuarantinePolicyStillAppendsOneEntry(t *testing.T) {
	h := newHarness(t, withPolicy(&quarantineOnReceiptPolicy{}))
	ctx := scoped(uuid.New(), uuid.New())

	if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 12)); err != nil {
		t.Fatal(err)
	}

	movement := h.ledger.only(t)
	if movement.MovementType != movementInbound {
		t.Errorf("movement type = %q, want INBOUND", movement.MovementType)
	}
	// Everything landed in quarantine, nothing in available.
	assertSnapshotsAgree(t, movement, 0, 0, 0, 12)
	if onHand(movement.After) != 12 {
		t.Errorf("after on-hand = %d, want 12", onHand(movement.After))
	}
}

// TestEveryMovementAppendsOneEntry walks the whole single-position vocabulary and
// checks the count and the movement type of each.
func TestEveryMovementAppendsOneEntry(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	received, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 100))
	if err != nil {
		t.Fatal(err)
	}
	id := received.ID

	steps := []struct {
		label        string
		movementType string
		run          func() error
	}{
		{"reserve", movementReservation, func() error {
			_, err := h.svc.ReserveStock(ctx, qtyReq(id, 30))
			return err
		}},
		{"allocate", movementAllocation, func() error {
			_, err := h.svc.AllocateStock(ctx, qtyReq(id, 10))
			return err
		}},
		{"deallocate", movementAllocation, func() error {
			_, err := h.svc.DeallocateStock(ctx, qtyReq(id, 10))
			return err
		}},
		{"release", movementReservation, func() error {
			_, err := h.svc.ReleaseReservation(ctx, qtyReq(id, 30))
			return err
		}},
		{"quarantine", movementQuarantine, func() error {
			_, err := h.svc.MoveToQuarantine(ctx, qtyReq(id, 5))
			return err
		}},
		{"release quarantine", movementQuarantine, func() error {
			_, err := h.svc.ReleaseFromQuarantine(ctx, qtyReq(id, 5))
			return err
		}},
		{"issue", movementOutbound, func() error {
			_, err := h.svc.IssueStock(ctx, qtyReq(id, 20))
			return err
		}},
	}

	for _, step := range steps {
		h.ledger.reset()
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.label, err)
		}
		movement := h.ledger.only(t)
		if movement.MovementType != step.movementType {
			t.Errorf("%s: movement type = %q, want %q",
				step.label, movement.MovementType, step.movementType)
		}
		if movement.PositionID != id {
			t.Errorf("%s: entry names the wrong position", step.label)
		}
	}
}

// TestBucketMovesRecordZeroOnHandDelta pins that a reservation is a SHUFFLE: the
// units change bucket but the total does not move, and the snapshots say so.
func TestBucketMovesRecordZeroOnHandDelta(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	received, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 40))
	if err != nil {
		t.Fatal(err)
	}
	h.ledger.reset()

	if _, err := h.svc.ReserveStock(ctx, qtyReq(received.ID, 15)); err != nil {
		t.Fatal(err)
	}
	movement := h.ledger.only(t)
	assertSnapshotsAgree(t, movement, -15, +15, 0, 0)
	if onHand(movement.Before) != onHand(movement.After) {
		t.Errorf("a reservation changed the total: %d -> %d",
			onHand(movement.Before), onHand(movement.After))
	}
}

// ---------- Adjustment reasons ----------

func TestAdjustmentReasonSelectsMovementType(t *testing.T) {
	cases := map[string]string{
		"CYCLE_COUNT":       movementCycleCount,
		"INITIAL_BALANCE":   movementInitial,
		"DAMAGE":            movementAdjustment,
		"SHRINKAGE":         movementAdjustment,
		"MANUAL_CORRECTION": movementAdjustment,
	}
	for adjustment, want := range cases {
		h := newHarness(t)
		ctx := scoped(uuid.New(), uuid.New())
		received, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 50))
		if err != nil {
			t.Fatal(err)
		}
		h.ledger.reset()

		counted := int64(42)
		_, err = h.svc.AdjustStock(ctx, dto.AdjustStockRequest{
			PositionID: received.ID,
			Quantity:   &counted,
			Type:       dto.AdjustmentType(adjustment),
			Reason:     "stock take",
		})
		if err != nil {
			t.Fatalf("%s: %v", adjustment, err)
		}
		movement := h.ledger.only(t)
		if movement.MovementType != want {
			t.Errorf("%s: movement type = %q, want %q", adjustment, movement.MovementType, want)
		}
		if movement.Reason != "stock take" {
			t.Errorf("%s: reason = %q, want the caller's reason", adjustment, movement.Reason)
		}
	}
}

// ---------- Transfer ----------

// TestTransferAppendsOneEntryPerPosition pins that a transfer is TWO entries —
// one per position, since an entry describes one position's balances — joined by
// a shared reference id so a reader can see they are one event.
func TestTransferAppendsOneEntryPerPosition(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	origin := newPlace()

	received, err := h.svc.ReceiveStock(ctx, receiveReq(origin, 60))
	if err != nil {
		t.Fatal(err)
	}
	h.ledger.reset()

	destinationLocation := uuid.New()
	if _, err := h.svc.TransferStock(ctx, dto.TransferStockRequest{
		FromPositionID: received.ID,
		ToWarehouseID:  origin.warehouse,
		ToLocationID:   destinationLocation,
		Quantity:       25,
	}); err != nil {
		t.Fatal(err)
	}

	movements := h.ledger.all()
	if len(movements) != 2 {
		t.Fatalf("transfer appended %d entries, want 2 (one per position)", len(movements))
	}
	for _, movement := range movements {
		if movement.MovementType != movementTransfer {
			t.Errorf("movement type = %q, want TRANSFER", movement.MovementType)
		}
		if movement.ReferenceID == nil {
			t.Fatal("a transfer leg carries no reference id, so the pair cannot be joined")
		}
	}
	if *movements[0].ReferenceID != *movements[1].ReferenceID {
		t.Error("the two legs do not share a reference id")
	}
	if movements[0].PositionID == movements[1].PositionID {
		t.Error("both legs name the same position")
	}

	// The origin loses what the destination gains: stock is conserved.
	out := onHand(movements[0].After) - onHand(movements[0].Before)
	in := onHand(movements[1].After) - onHand(movements[1].Before)
	if out != -25 || in != 25 {
		t.Errorf("transfer deltas = %d / %d, want -25 / +25", out, in)
	}
}

// ---------- Transactionality ----------

// TestLedgerFailureRollsBackTheMovement is the guarantee that makes the ledger
// trustworthy: if the entry cannot be written, the stock does not move.
func TestLedgerFailureRollsBackTheMovement(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.ledger.fail(errInfrastructure)

	if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10)); err == nil {
		t.Fatal("a receive succeeded despite the ledger append failing")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.repo.count() != 0 {
		t.Error("the position survived a failed ledger append")
	}
	if h.ledger.count() != 0 {
		t.Error("a failed append left an entry behind")
	}
	if h.events.count() != 0 {
		t.Error("a rolled-back movement published a domain event")
	}
}

// TestFailedMovementAppendsNothing pins the other direction: when the AGGREGATE
// refuses, no entry is written either — the ledger records movements, not
// attempts.
func TestFailedMovementAppendsNothing(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	received, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 5))
	if err != nil {
		t.Fatal(err)
	}
	h.ledger.reset()

	// Issuing more than exists is refused by the aggregate.
	if _, err := h.svc.IssueStock(ctx, qtyReq(received.ID, 500)); err == nil {
		t.Fatal("an over-issue was accepted")
	}
	if h.ledger.count() != 0 {
		t.Errorf("a refused movement appended %d entries", h.ledger.count())
	}
}

// TestRepositoryFailureAppendsNothing covers the ordering: the entry is written
// after the position is persisted, so a persist failure cannot leave an orphan
// record of a movement that never landed.
func TestRepositoryFailureAppendsNothing(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Update", errInfrastructure)

	if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 5)); err == nil {
		t.Fatal("a receive succeeded despite the repository failing")
	}
	if h.ledger.count() != 0 {
		t.Errorf("a failed persist appended %d entries", h.ledger.count())
	}
}

// ---------- Replay ----------

// TestReplayOfEntriesReproducesThePosition is the reconciliation property an
// audit trail exists for: applying every recorded delta in order must land on the
// balances the position actually holds.
func TestReplayOfEntriesReproducesThePosition(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	received, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 100))
	if err != nil {
		t.Fatal(err)
	}
	id := received.ID

	for _, step := range []func() error{
		func() error { _, err := h.svc.ReserveStock(ctx, qtyReq(id, 40)); return err },
		func() error { _, err := h.svc.AllocateStock(ctx, qtyReq(id, 25)); return err },
		func() error { _, err := h.svc.MoveToQuarantine(ctx, qtyReq(id, 10)); return err },
		func() error { _, err := h.svc.IssueStock(ctx, qtyReq(id, 15)); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}

	var replay ledgerdto.BucketSnapshotRequest
	for index, movement := range h.ledger.all() {
		// Each entry's BEFORE must equal the running total so far, or the chain
		// has a gap — a movement that happened without being recorded.
		if movement.Before != replay {
			t.Fatalf("entry %d starts from %+v but the replay is at %+v", index, movement.Before, replay)
		}
		replay = ledgerdto.BucketSnapshotRequest{
			Available:   replay.Available + (movement.After.Available - movement.Before.Available),
			Reserved:    replay.Reserved + (movement.After.Reserved - movement.Before.Reserved),
			Allocated:   replay.Allocated + (movement.After.Allocated - movement.Before.Allocated),
			Quarantined: replay.Quarantined + (movement.After.Quarantined - movement.Before.Quarantined),
		}
	}

	current, err := h.svc.GetInventoryPosition(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Available != current.Available || replay.Reserved != current.Reserved ||
		replay.Allocated != current.Allocated || replay.Quarantined != current.Quarantined {
		t.Fatalf("replay = %+v but the position holds avail:%d res:%d alloc:%d quar:%d",
			replay, current.Available, current.Reserved, current.Allocated, current.Quarantined)
	}
	if onHand(replay) != current.OnHand {
		t.Errorf("replayed on-hand = %d, position reports %d", onHand(replay), current.OnHand)
	}
}

// ---------- Test doubles ----------

// quarantineOnReceiptPolicy holds every receipt for inspection.
type quarantineOnReceiptPolicy struct{ DefaultStockPolicy }

func (quarantineOnReceiptPolicy) RequireQuarantineOnReceipt(
	_ context.Context, _, _ uuid.UUID,
) (bool, error) {
	return true, nil
}
