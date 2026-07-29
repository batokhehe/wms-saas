// Package entity holds the InventoryLedgerEntry aggregate and its value objects.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror and
// the shared identity types, and nothing from any web or persistence framework.
//
// The ledger is APPEND-ONLY. It records that a stock transition happened; it
// never owns stock. InventoryPosition remains the only source of truth for
// quantities — every value here is a SNAPSHOT of what the position reported at
// the moment of the movement, kept so history can be read without replaying it.
package entity

import (
	"strings"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// MovementType
// ---------------------------------------------------------------------------

// MovementType names the kind of transition an entry records.
type MovementType string

const (
	// MovementInitialBalance seeds an opening balance at go-live.
	MovementInitialBalance MovementType = "INITIAL_BALANCE"

	// MovementInbound is stock arriving into a position.
	MovementInbound MovementType = "INBOUND"

	// MovementOutbound is stock leaving a position.
	MovementOutbound MovementType = "OUTBOUND"

	// MovementTransfer is stock moving between two positions. A transfer produces
	// TWO entries — one per position — because each side is a distinct change to
	// a distinct position, and a single row could not carry both.
	MovementTransfer MovementType = "TRANSFER"

	// MovementReservation is a soft promise being made or returned.
	MovementReservation MovementType = "RESERVATION"

	// MovementAllocation is a promise being hardened into a task assignment, or
	// released back.
	MovementAllocation MovementType = "ALLOCATION"

	// MovementAdjustment is an absolute correction without a physical count.
	MovementAdjustment MovementType = "ADJUSTMENT"

	// MovementQuarantine is stock being held back from use, or released.
	MovementQuarantine MovementType = "QUARANTINE"

	// MovementCycleCount is a reconciliation backed by a physical count.
	MovementCycleCount MovementType = "CYCLE_COUNT"
)

// Valid reports whether the movement type is a known value.
func (m MovementType) Valid() bool {
	switch m {
	case MovementInitialBalance, MovementInbound, MovementOutbound, MovementTransfer,
		MovementReservation, MovementAllocation, MovementAdjustment,
		MovementQuarantine, MovementCycleCount:
		return true
	default:
		return false
	}
}

// String renders the movement type.
func (m MovementType) String() string { return string(m) }

// ---------------------------------------------------------------------------
// MovementContext
// ---------------------------------------------------------------------------

// MovementContext is the PROVENANCE of a movement: what business document caused
// it, and why.
//
// It is one value object rather than four loose fields because the four are only
// meaningful together — a reference id without its type cannot be resolved, and a
// document number with neither is unattributable. Bundling them means an entry
// either carries a coherent provenance or none at all.
//
// Every field is optional: an operator correction has a reason and no document,
// while a receipt against a purchase order has both.
type MovementContext struct {
	referenceType  string
	referenceID    *uuid.UUID
	documentNumber string
	reason         string
}

// NewMovementContext composes a provenance, trimming and length-checking each
// field. A reference id supplied without a reference type is rejected: an
// identifier nobody can resolve is worse than no identifier.
func NewMovementContext(referenceType string, referenceID *uuid.UUID, documentNumber, reason string) (MovementContext, error) {
	referenceType = strings.TrimSpace(referenceType)
	documentNumber = strings.TrimSpace(documentNumber)
	reason = strings.TrimSpace(reason)

	if len(referenceType) > 64 {
		return MovementContext{}, apperror.Validation("reference type must be at most 64 characters")
	}
	if len(documentNumber) > 64 {
		return MovementContext{}, apperror.Validation("document number must be at most 64 characters")
	}
	if len(reason) > 500 {
		return MovementContext{}, apperror.Validation("reason must be at most 500 characters")
	}
	if referenceID != nil && *referenceID != uuid.Nil && referenceType == "" {
		return MovementContext{}, apperror.Validation("a reference id requires a reference type")
	}

	var ref *uuid.UUID
	if referenceID != nil && *referenceID != uuid.Nil {
		id := *referenceID
		ref = &id
	}

	return MovementContext{
		referenceType:  referenceType,
		referenceID:    ref,
		documentNumber: documentNumber,
		reason:         reason,
	}, nil
}

// EmptyMovementContext is a movement with no recorded provenance.
func EmptyMovementContext() MovementContext { return MovementContext{} }

// ReferenceType returns the kind of document that caused the movement.
func (c MovementContext) ReferenceType() string { return c.referenceType }

// ReferenceID returns a copy of the causing document's id, or nil.
func (c MovementContext) ReferenceID() *uuid.UUID {
	if c.referenceID == nil {
		return nil
	}
	id := *c.referenceID
	return &id
}

// DocumentNumber returns the human-facing document number.
func (c MovementContext) DocumentNumber() string { return c.documentNumber }

// Reason returns the free-text justification.
func (c MovementContext) Reason() string { return c.reason }

// HasReference reports whether a resolvable document reference is recorded.
func (c MovementContext) HasReference() bool { return c.referenceID != nil }

// ---------------------------------------------------------------------------
// Bucket snapshots
// ---------------------------------------------------------------------------

// buckets is the shared shape of a four-balance snapshot. It is unexported so
// only BeforeBucket and AfterBucket can be constructed from it — the ledger never
// deals in an unlabelled snapshot.
type buckets struct {
	available   int64
	reserved    int64
	allocated   int64
	quarantined int64
}

func newBuckets(available, reserved, allocated, quarantined int64) (buckets, error) {
	if available < 0 || reserved < 0 || allocated < 0 || quarantined < 0 {
		return buckets{}, apperror.Validation("bucket balances cannot be negative")
	}
	return buckets{
		available:   available,
		reserved:    reserved,
		allocated:   allocated,
		quarantined: quarantined,
	}, nil
}

func (b buckets) onHand() int64 {
	return b.available + b.reserved + b.allocated + b.quarantined
}

// BeforeBucket is the position's balances as they stood BEFORE the movement.
//
// It is a distinct type from AfterBucket on purpose: the two are structurally
// identical, and a plain pair of the same type would let a caller transpose them
// silently — inverting every delta in the ledger with nothing to catch it.
type BeforeBucket struct{ buckets }

// NewBeforeBucket builds the pre-movement snapshot.
func NewBeforeBucket(available, reserved, allocated, quarantined int64) (BeforeBucket, error) {
	b, err := newBuckets(available, reserved, allocated, quarantined)
	if err != nil {
		return BeforeBucket{}, err
	}
	return BeforeBucket{buckets: b}, nil
}

// Available returns the pre-movement available balance.
func (b BeforeBucket) Available() int64 { return b.available }

// Reserved returns the pre-movement reserved balance.
func (b BeforeBucket) Reserved() int64 { return b.reserved }

// Allocated returns the pre-movement allocated balance.
func (b BeforeBucket) Allocated() int64 { return b.allocated }

// Quarantined returns the pre-movement quarantined balance.
func (b BeforeBucket) Quarantined() int64 { return b.quarantined }

// OnHand returns the pre-movement total, derived from the four balances.
func (b BeforeBucket) OnHand() int64 { return b.onHand() }

// AfterBucket is the position's balances as they stood AFTER the movement.
type AfterBucket struct{ buckets }

// NewAfterBucket builds the post-movement snapshot.
func NewAfterBucket(available, reserved, allocated, quarantined int64) (AfterBucket, error) {
	b, err := newBuckets(available, reserved, allocated, quarantined)
	if err != nil {
		return AfterBucket{}, err
	}
	return AfterBucket{buckets: b}, nil
}

// Available returns the post-movement available balance.
func (b AfterBucket) Available() int64 { return b.available }

// Reserved returns the post-movement reserved balance.
func (b AfterBucket) Reserved() int64 { return b.reserved }

// Allocated returns the post-movement allocated balance.
func (b AfterBucket) Allocated() int64 { return b.allocated }

// Quarantined returns the post-movement quarantined balance.
func (b AfterBucket) Quarantined() int64 { return b.quarantined }

// OnHand returns the post-movement total, derived from the four balances.
func (b AfterBucket) OnHand() int64 { return b.onHand() }

// ---------------------------------------------------------------------------
// BucketDelta
// ---------------------------------------------------------------------------

// BucketDelta is the signed change each bucket underwent.
//
// It is DERIVED from the two snapshots, never supplied by a caller. That is the
// whole point: a delta a caller could set independently would be free to
// contradict the before/after pair it sits beside, and the ledger's only job is
// to be a record nobody can argue with.
type BucketDelta struct {
	available   int64
	reserved    int64
	allocated   int64
	quarantined int64
	onHand      int64
}

// NewBucketDelta computes the change between two snapshots.
func NewBucketDelta(before BeforeBucket, after AfterBucket) BucketDelta {
	return BucketDelta{
		available:   after.available - before.available,
		reserved:    after.reserved - before.reserved,
		allocated:   after.allocated - before.allocated,
		quarantined: after.quarantined - before.quarantined,
		onHand:      after.onHand() - before.onHand(),
	}
}

// Available returns the signed change in available stock.
func (d BucketDelta) Available() int64 { return d.available }

// Reserved returns the signed change in reserved stock.
func (d BucketDelta) Reserved() int64 { return d.reserved }

// Allocated returns the signed change in allocated stock.
func (d BucketDelta) Allocated() int64 { return d.allocated }

// Quarantined returns the signed change in quarantined stock.
func (d BucketDelta) Quarantined() int64 { return d.quarantined }

// OnHand returns the signed change in total stock. It is zero for a movement
// that only shuffles stock between buckets — a reservation or an allocation —
// and non-zero only when stock genuinely entered or left the position.
func (d BucketDelta) OnHand() int64 { return d.onHand }

// IsBucketShuffle reports whether the movement redistributed stock without
// changing the total.
func (d BucketDelta) IsBucketShuffle() bool { return d.onHand == 0 }

// IsZero reports whether nothing changed at all.
func (d BucketDelta) IsZero() bool {
	return d.available == 0 && d.reserved == 0 && d.allocated == 0 && d.quarantined == 0
}
