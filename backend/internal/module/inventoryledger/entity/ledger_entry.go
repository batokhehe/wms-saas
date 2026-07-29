package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// InventoryLedgerEntry is the AGGREGATE ROOT of the inventory ledger: one
// immutable record that a stock transition happened.
//
// # Append-only by construction
//
// There are no setters, no mutating behaviours, and no state machine — only a
// factory, a reconstitutor and getters. Immutability is not a rule the code
// remembers to follow; it is the absence of any way to break it. Nothing here
// can be changed after it is created, so the ledger cannot be rewritten by the
// application even in principle. The database enforces the same rule with a
// trigger that rejects UPDATE and DELETE.
//
// # It never owns stock
//
// Every quantity on an entry is a SNAPSHOT of what InventoryPosition reported at
// the moment of the movement. The ledger is a witness, not a source of truth: no
// process reads a balance from here to decide whether stock may move. That is
// why the entry carries the position id — the authoritative record is always one
// lookup away.
//
// It has no child entities: an entry is a single fact, and a movement affecting
// two positions (a transfer) produces two entries rather than one with two sides.
type InventoryLedgerEntry struct {
	ledgerID  uuid.UUID
	companyID uuid.UUID

	// Where the movement happened. These are DENORMALISED from the position so a
	// ledger query can filter by product or warehouse without joining a table
	// whose rows may since have moved on.
	positionID  uuid.UUID
	productID   uuid.UUID
	warehouseID uuid.UUID
	locationID  uuid.UUID

	lotNumber    string
	serialNumber string

	// ownerID names the party the stock belongs to when it is not the operating
	// company — consignment or 3PL stock. Nil means the company owns it.
	ownerID *uuid.UUID

	movementType MovementType
	context      MovementContext

	actorID uuid.UUID

	before BeforeBucket
	after  AfterBucket
	delta  BucketDelta

	occurredAt time.Time
}

// NewLedgerEntry is the FACTORY, and the only way an entry comes into existence.
//
// The delta is COMPUTED from the two snapshots rather than accepted, so a caller
// cannot record a change that contradicts the balances beside it.
func NewLedgerEntry(
	ledgerID, companyID uuid.UUID,
	positionID, productID, warehouseID, locationID uuid.UUID,
	lotNumber, serialNumber string,
	ownerID *uuid.UUID,
	movementType MovementType,
	movementContext MovementContext,
	actorID uuid.UUID,
	before BeforeBucket,
	after AfterBucket,
	occurredAt time.Time,
) (*InventoryLedgerEntry, error) {
	if ledgerID == uuid.Nil || companyID == uuid.Nil || positionID == uuid.Nil ||
		productID == uuid.Nil || warehouseID == uuid.Nil || locationID == uuid.Nil {
		return nil, apperror.Validation("ledger, company, position, product, warehouse and location ids are required")
	}
	if actorID == uuid.Nil {
		return nil, apperror.Validation("actor id is required")
	}
	if !movementType.Valid() {
		return nil, apperror.Validation("invalid movement type")
	}
	if occurredAt.IsZero() {
		return nil, apperror.Validation("occurred-at timestamp is required")
	}

	lotNumber = strings.TrimSpace(lotNumber)
	serialNumber = strings.TrimSpace(serialNumber)
	if len(lotNumber) > 64 {
		return nil, apperror.Validation("lot number must be at most 64 characters")
	}
	if len(serialNumber) > 128 {
		return nil, apperror.Validation("serial number must be at most 128 characters")
	}
	// A single unit cannot simultaneously be a batch: the position it describes
	// is either lot-tracked or serial-tracked, never both.
	if lotNumber != "" && serialNumber != "" {
		return nil, apperror.Validation("an entry cannot carry both a lot and a serial number")
	}

	return &InventoryLedgerEntry{
		ledgerID:     ledgerID,
		companyID:    companyID,
		positionID:   positionID,
		productID:    productID,
		warehouseID:  warehouseID,
		locationID:   locationID,
		lotNumber:    lotNumber,
		serialNumber: serialNumber,
		ownerID:      copyID(ownerID),
		movementType: movementType,
		context:      movementContext,
		actorID:      actorID,
		before:       before,
		after:        after,
		delta:        NewBucketDelta(before, after),
		occurredAt:   occurredAt,
	}, nil
}

// Reconstitute restores a stored entry.
//
// Unlike every other aggregate in the system it raises no events and has no
// version: an entry is written once and never updated, so there is no
// optimistic-lock token to restore and no transition to announce.
//
// The delta is RECOMPUTED here rather than read from the row. If a stored delta
// ever disagreed with its own before/after columns, recomputing silently repairs
// the read instead of propagating the contradiction.
func Reconstitute(
	ledgerID, companyID uuid.UUID,
	positionID, productID, warehouseID, locationID uuid.UUID,
	lotNumber, serialNumber string,
	ownerID *uuid.UUID,
	movementType MovementType,
	movementContext MovementContext,
	actorID uuid.UUID,
	before BeforeBucket,
	after AfterBucket,
	occurredAt time.Time,
) (*InventoryLedgerEntry, error) {
	if ledgerID == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("invalid persisted ledger entry")
	}
	if !movementType.Valid() {
		return nil, apperror.Validation("invalid persisted ledger entry")
	}

	return &InventoryLedgerEntry{
		ledgerID:     ledgerID,
		companyID:    companyID,
		positionID:   positionID,
		productID:    productID,
		warehouseID:  warehouseID,
		locationID:   locationID,
		lotNumber:    strings.TrimSpace(lotNumber),
		serialNumber: strings.TrimSpace(serialNumber),
		ownerID:      copyID(ownerID),
		movementType: movementType,
		context:      movementContext,
		actorID:      actorID,
		before:       before,
		after:        after,
		delta:        NewBucketDelta(before, after),
		occurredAt:   occurredAt,
	}, nil
}

// copyID defensively copies an optional identifier so a caller cannot mutate the
// entry through the pointer it handed in.
func copyID(v *uuid.UUID) *uuid.UUID {
	if v == nil || *v == uuid.Nil {
		return nil
	}
	id := *v
	return &id
}

// ---------------------------------------------------------------------------
// Accessors (read-only — the entry exposes nothing else)
// ---------------------------------------------------------------------------

// LedgerID returns the entry identity.
func (e *InventoryLedgerEntry) LedgerID() uuid.UUID { return e.ledgerID }

// CompanyID returns the owning tenant.
func (e *InventoryLedgerEntry) CompanyID() uuid.UUID { return e.companyID }

// PositionID returns the inventory position the movement applied to — the
// authoritative record of the stock this entry merely witnesses.
func (e *InventoryLedgerEntry) PositionID() uuid.UUID { return e.positionID }

// ProductID returns the product.
func (e *InventoryLedgerEntry) ProductID() uuid.UUID { return e.productID }

// WarehouseID returns the warehouse.
func (e *InventoryLedgerEntry) WarehouseID() uuid.UUID { return e.warehouseID }

// LocationID returns the storage location.
func (e *InventoryLedgerEntry) LocationID() uuid.UUID { return e.locationID }

// LotNumber returns the lot, or "" when the stock is not lot-tracked.
func (e *InventoryLedgerEntry) LotNumber() string { return e.lotNumber }

// SerialNumber returns the serial, or "" when the stock is not serial-tracked.
func (e *InventoryLedgerEntry) SerialNumber() string { return e.serialNumber }

// OwnerID returns a copy of the stock owner, or nil when the company owns it.
func (e *InventoryLedgerEntry) OwnerID() *uuid.UUID { return copyID(e.ownerID) }

// MovementType returns the kind of transition recorded.
func (e *InventoryLedgerEntry) MovementType() MovementType { return e.movementType }

// Context returns the movement's provenance.
func (e *InventoryLedgerEntry) Context() MovementContext { return e.context }

// ReferenceType returns the causing document's kind.
func (e *InventoryLedgerEntry) ReferenceType() string { return e.context.ReferenceType() }

// ReferenceID returns the causing document's id, or nil.
func (e *InventoryLedgerEntry) ReferenceID() *uuid.UUID { return e.context.ReferenceID() }

// DocumentNumber returns the human-facing document number.
func (e *InventoryLedgerEntry) DocumentNumber() string { return e.context.DocumentNumber() }

// Reason returns the free-text justification.
func (e *InventoryLedgerEntry) Reason() string { return e.context.Reason() }

// ActorID returns who performed the movement.
func (e *InventoryLedgerEntry) ActorID() uuid.UUID { return e.actorID }

// Before returns the pre-movement balances.
func (e *InventoryLedgerEntry) Before() BeforeBucket { return e.before }

// After returns the post-movement balances.
func (e *InventoryLedgerEntry) After() AfterBucket { return e.after }

// Delta returns the computed change.
func (e *InventoryLedgerEntry) Delta() BucketDelta { return e.delta }

// OccurredAt returns when the movement happened. This is BUSINESS time, supplied
// by the caller, not the moment the row was written — a backdated correction
// records when the stock actually moved.
func (e *InventoryLedgerEntry) OccurredAt() time.Time { return e.occurredAt }
