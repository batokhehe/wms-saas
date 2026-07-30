package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	ledgerdto "github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
)

// This file is the inventory module's side of the ledger integration.
//
// # The guarantee
//
// Every movement that changes a position appends EXACTLY ONE ledger entry, and
// the append happens inside the movement's own transaction. The ledger service
// joins that transaction through a SAVEPOINT rather than opening a second one,
// so the position change and the entry that witnesses it commit or roll back
// together. A position can never move without a record, and a record can never
// describe a move that was rolled back.
//
// # Why the snapshots are taken here
//
// The ledger derives its delta from the two snapshots it is given, so it can
// never record a change that contradicts the balances it reports. That only
// works if BEFORE is read before the aggregate behaviour runs and AFTER is read
// after it — which is something only the caller can do. Hence the shape below:
// snapshot, mutate, snapshot, record.

// The ledger's closed movement-type vocabulary.
const (
	movementInbound     = "INBOUND"
	movementOutbound    = "OUTBOUND"
	movementTransfer    = "TRANSFER"
	movementReservation = "RESERVATION"
	movementAllocation  = "ALLOCATION"
	movementAdjustment  = "ADJUSTMENT"
	movementQuarantine  = "QUARANTINE"
	movementCycleCount  = "CYCLE_COUNT"
	movementInitial     = "INITIAL_BALANCE"
)

// movementRef is the provenance recorded alongside an entry.
//
// Until a purchase/sales order carries the movement, ReferenceType names the
// INVENTORY OPERATION that caused it — a greppable, stable value that costs no
// API change. ReferenceID exists so a multi-position operation can be recognised
// as one event: a transfer's two legs share it.
type movementRef struct {
	kind     string
	id       *uuid.UUID
	document string
	reason   string
}

// snapshot reads a position's four balances.
func snapshot(p *entity.InventoryPosition) ledgerdto.BucketSnapshotRequest {
	return ledgerdto.BucketSnapshotRequest{
		Available:   p.Available().Value(),
		Reserved:    p.Reserved().Value(),
		Allocated:   p.Allocated().Value(),
		Quarantined: p.Quarantined().Value(),
	}
}

// recordMovement appends one immutable entry describing one position transition.
//
// It is called INSIDE the caller's transaction and its error is returned
// unwrapped, so a ledger failure aborts the movement rather than leaving stock
// that nothing witnessed.
func (s *Service) recordMovement(
	ctx context.Context,
	position *entity.InventoryPosition,
	movementType string,
	before, after ledgerdto.BucketSnapshotRequest,
	ref movementRef,
	occurredAt time.Time,
) error {
	attributes := position.Attributes()

	return s.ledger.RecordMovement(ctx, ledgerdto.RecordMovementRequest{
		PositionID:  position.ID(),
		ProductID:   position.ProductID(),
		WarehouseID: position.WarehouseID(),
		LocationID:  position.LocationID(),

		LotNumber:    attributes.Lot().String(),
		SerialNumber: attributes.Serial().String(),

		MovementType: movementType,

		ReferenceType:  ref.kind,
		ReferenceID:    ref.id,
		DocumentNumber: ref.document,
		Reason:         ref.reason,

		Before: before,
		After:  after,

		// Business time is the movement's own clock reading, so the entry and the
		// position agree on when the stock moved.
		OccurredAt: occurredAt,
	})
}

// adjustmentMovementType maps an adjustment reason onto the ledger's vocabulary.
//
// A cycle count and an opening balance are their own movement types because a
// report grouping by movement must be able to separate "we counted" and "we went
// live" from "someone corrected this by hand".
func adjustmentMovementType(adjustment string) string {
	switch adjustment {
	case movementCycleCount:
		return movementCycleCount
	case movementInitial:
		return movementInitial
	default:
		return movementAdjustment
	}
}
