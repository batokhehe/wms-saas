package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// InventoryPosition is the AGGREGATE ROOT for stock. It is the stock of one
// product, with one set of stock attributes, in one storage location, and it
// owns every transition that stock can undergo.
//
// # The bucket model
//
// A position holds four balances, and every unit of stock sits in exactly one:
//
//	available   — unencumbered; the only stock that may be freely taken
//	reserved    — softly promised to an order, not yet assigned to a task
//	allocated   — hard-assigned to a specific pick or task
//	quarantined — held back from use pending inspection or disposition
//
// OnHand is DERIVED as their sum and is never stored, so the four balances and
// the total cannot disagree. That is the whole point of the model: there is no
// separate total to keep in step.
//
// Every behaviour is either a movement across the position boundary
// (Receive adds to available, Issue removes from available) or a move BETWEEN
// two buckets (Reserve, Release, Allocate, Deallocate, MoveToQuarantine,
// ReleaseFromQuarantine). Because each bucket refuses to go negative, a
// bucket-to-bucket move cannot invent or destroy stock — it can only fail.
//
// # Invariants
//
//	every bucket >= 0                        (QuantityBucket enforces it)
//	OnHand == available+reserved+allocated+quarantined   (derived, not stored)
//	stock leaves only from available          (Issue)
//	encumbered stock is reachable only through its own release path
//	SERIAL => OnHand <= 1                     (a serial is one physical unit)
//
// Cross-aggregate rules — the product, warehouse and location exist — are
// enforced by the application service through its verifiers; the aggregate holds
// the verified ids in its StockKey.
type InventoryPosition struct {
	id  uuid.UUID
	key StockKey

	available   QuantityBucket
	reserved    QuantityBucket
	allocated   QuantityBucket
	quarantined QuantityBucket

	version uint64

	createdBy uuid.UUID
	updatedBy uuid.UUID
	createdAt time.Time
	updatedAt time.Time

	events []Event
}

// NewInventoryPosition is the FACTORY. It opens an EMPTY position: stock arrives
// only through Receive, so there is exactly one way a balance can come into
// existence and it always raises a StockReceived event.
func NewInventoryPosition(
	id uuid.UUID,
	key StockKey,
	actor uuid.UUID,
	now time.Time,
) (*InventoryPosition, error) {
	if id == uuid.Nil || actor == uuid.Nil {
		return nil, apperror.Validation("position and actor ids are required")
	}
	if key.CompanyID() == uuid.Nil {
		return nil, apperror.Validation("stock key is required")
	}

	p := &InventoryPosition{
		id:        id,
		key:       key,
		version:   1,
		createdBy: actor,
		updatedBy: actor,
		createdAt: now,
		updatedAt: now,
	}

	p.record(EventPositionCreated, actor, now, map[string]any{
		"product_id":   key.ProductID().String(),
		"warehouse_id": key.WarehouseID().String(),
		"location_id":  key.LocationID().String(),
		"tracking":     key.Attributes().Tracking().String(),
		"lot":          key.Attributes().Lot().String(),
		"serial":       key.Attributes().Serial().String(),
	})

	return p, nil
}

// Reconstitute restores stored state WITHOUT raising events, re-validating the
// persisted balances so a corrupt row is refused rather than silently trusted.
func Reconstitute(
	id uuid.UUID,
	key StockKey,
	available, reserved, allocated, quarantined QuantityBucket,
	version uint64,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*InventoryPosition, error) {
	if version == 0 || id == uuid.Nil || key.CompanyID() == uuid.Nil {
		return nil, apperror.Validation("invalid persisted position state")
	}

	p := &InventoryPosition{
		id:          id,
		key:         key,
		available:   available,
		reserved:    reserved,
		allocated:   allocated,
		quarantined: quarantined,
		version:     version,
		createdBy:   createdBy,
		updatedBy:   updatedBy,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}

	if err := p.assertSerialLimit(); err != nil {
		return nil, err
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Behaviours: across the position boundary
// ---------------------------------------------------------------------------

// Receive books stock into the position. Arriving stock is always unencumbered,
// so it lands in available.
func (p *InventoryPosition) Receive(amount Quantity, actor uuid.UUID, now time.Time) error {
	next, err := p.available.Add(amount)
	if err != nil {
		return err
	}
	// Checked against the PROSPECTIVE total, so a serial position can never be
	// pushed past one unit even by a first receipt.
	if err := p.assertSerialLimitWith(next, p.reserved, p.allocated, p.quarantined); err != nil {
		return err
	}

	p.available = next
	p.touch(actor, now)
	p.record(EventStockReceived, actor, now, p.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// Issue removes stock from the position.
//
// It draws from AVAILABLE only. Stock that is reserved, allocated or quarantined
// is spoken for, and taking it without first releasing it would silently break
// the promise that put it there — so the caller must Release, Deallocate or
// ReleaseFromQuarantine first. That refusal is the point of the bucket model.
func (p *InventoryPosition) Issue(amount Quantity, actor uuid.UUID, now time.Time) error {
	next, err := p.available.Sub(amount)
	if err != nil {
		return apperror.Conflict("insufficient available stock to issue")
	}
	p.available = next
	p.touch(actor, now)
	p.record(EventStockIssued, actor, now, p.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// ---------------------------------------------------------------------------
// Behaviours: bucket-to-bucket moves
// ---------------------------------------------------------------------------

// Reserve softly promises available stock to an order.
func (p *InventoryPosition) Reserve(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.available, &p.reserved, amount, EventStockReserved,
		"insufficient available stock to reserve", actor, now)
}

// Release returns a soft promise to the available pool.
func (p *InventoryPosition) Release(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.reserved, &p.available, amount, EventStockReleased,
		"cannot release more than is reserved", actor, now)
}

// Allocate hardens a reservation into an assignment against a specific task.
//
// It draws from RESERVED, not from available. Allocation is the second step of a
// two-stage commitment — stock is first promised to an order, then assigned to
// the task that will pick it — so allocating stock that was never reserved would
// skip the promise it is supposed to harden.
func (p *InventoryPosition) Allocate(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.reserved, &p.allocated, amount, EventStockAllocated,
		"insufficient reserved stock to allocate", actor, now)
}

// Deallocate returns an assignment to the reserved pool, undoing Allocate.
func (p *InventoryPosition) Deallocate(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.allocated, &p.reserved, amount, EventStockDeallocated,
		"cannot deallocate more than is allocated", actor, now)
}

// MoveToQuarantine holds available stock back from use.
//
// A quantity, not a whole-position flag: a damaged carton out of a pallet puts
// part of the position under inspection while the rest stays sellable.
func (p *InventoryPosition) MoveToQuarantine(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.available, &p.quarantined, amount, EventStockQuarantined,
		"insufficient available stock to quarantine", actor, now)
}

// ReleaseFromQuarantine returns held stock to the available pool.
func (p *InventoryPosition) ReleaseFromQuarantine(amount Quantity, actor uuid.UUID, now time.Time) error {
	return p.move(&p.quarantined, &p.available, amount, EventStockReleasedFromQuarantine,
		"cannot release more than is quarantined", actor, now)
}

// move transfers a quantity between two buckets, applying BOTH sides or neither.
//
// Both new balances are computed before either is written, so a failure on the
// receiving side cannot leave the source already debited. This is the single
// place bucket arithmetic happens; every transition above delegates to it, which
// is why no behaviour can invent or destroy stock.
func (p *InventoryPosition) move(
	from, to *QuantityBucket,
	amount Quantity,
	event EventName,
	shortfall string,
	actor uuid.UUID,
	now time.Time,
) error {
	debited, err := from.Sub(amount)
	if err != nil {
		return apperror.Conflict(shortfall)
	}
	credited, err := to.Add(amount)
	if err != nil {
		return err
	}

	*from, *to = debited, credited
	p.touch(actor, now)
	p.record(event, actor, now, p.withState(map[string]any{"amount": amount.Value()}))
	return nil
}

// Adjust reconciles the position to a counted total, with the reason that
// justifies it.
//
// Only AVAILABLE absorbs the variance: encumbered stock is spoken for, so a
// count is refused outright when it falls below what is already reserved,
// allocated and quarantined — that is a discrepancy an operator must resolve by
// releasing the promises, not one a reconciliation may silently absorb.
func (p *InventoryPosition) Adjust(counted int64, reason string, actor uuid.UUID, now time.Time) error {
	if counted < 0 {
		return apperror.Validation("counted quantity cannot be negative")
	}

	encumbered := p.reserved.Value() + p.allocated.Value() + p.quarantined.Value()
	if counted < encumbered {
		return apperror.Conflict("counted quantity is below the reserved, allocated and quarantined stock")
	}

	nextAvailable, err := NewQuantityBucket(counted - encumbered)
	if err != nil {
		return err
	}
	if err := p.assertSerialLimitWith(nextAvailable, p.reserved, p.allocated, p.quarantined); err != nil {
		return err
	}

	previous := p.OnHand()
	p.available = nextAvailable
	p.touch(actor, now)
	p.record(EventStockAdjusted, actor, now, p.withState(map[string]any{
		"previous_on_hand": previous,
		"counted":          counted,
		"variance":         counted - previous,
		"reason":           reason,
	}))
	return nil
}

// ---------------------------------------------------------------------------
// Invariants
// ---------------------------------------------------------------------------

// assertSerialLimit enforces "a serial is one physical unit" on current state.
func (p *InventoryPosition) assertSerialLimit() error {
	return p.assertSerialLimitWith(p.available, p.reserved, p.allocated, p.quarantined)
}

// assertSerialLimitWith enforces the serial rule against a PROSPECTIVE set of
// balances, so a behaviour can test the outcome before committing to it.
func (p *InventoryPosition) assertSerialLimitWith(available, reserved, allocated, quarantined QuantityBucket) error {
	if !p.key.Attributes().IsSerialised() {
		return nil
	}
	total := available.Value() + reserved.Value() + allocated.Value() + quarantined.Value()
	if total > 1 {
		return apperror.Conflict("serial-tracked stock cannot exceed one unit")
	}
	return nil
}

// touch records who last changed the position and when. Version is NOT advanced
// here: it belongs to the persistence layer, and the aggregate exposes it
// read-only.
func (p *InventoryPosition) touch(actor uuid.UUID, now time.Time) {
	p.updatedBy = actor
	p.updatedAt = now
}

// ---------------------------------------------------------------------------
// Accessors (read-only)
// ---------------------------------------------------------------------------

// ID returns the position identity.
func (p *InventoryPosition) ID() uuid.UUID { return p.id }

// Key returns the position's full stock key.
func (p *InventoryPosition) Key() StockKey { return p.key }

// CompanyID returns the owning tenant.
func (p *InventoryPosition) CompanyID() uuid.UUID { return p.key.CompanyID() }

// WarehouseID returns the warehouse.
func (p *InventoryPosition) WarehouseID() uuid.UUID { return p.key.WarehouseID() }

// LocationID returns the storage location.
func (p *InventoryPosition) LocationID() uuid.UUID { return p.key.LocationID() }

// ProductID returns the product.
func (p *InventoryPosition) ProductID() uuid.UUID { return p.key.ProductID() }

// Attributes returns the individuating stock attributes.
func (p *InventoryPosition) Attributes() StockAttributes { return p.key.Attributes() }

// Available returns the unencumbered balance.
func (p *InventoryPosition) Available() QuantityBucket { return p.available }

// Reserved returns the softly-promised balance.
func (p *InventoryPosition) Reserved() QuantityBucket { return p.reserved }

// Allocated returns the task-assigned balance.
func (p *InventoryPosition) Allocated() QuantityBucket { return p.allocated }

// Quarantined returns the held balance.
func (p *InventoryPosition) Quarantined() QuantityBucket { return p.quarantined }

// OnHand is the total physical stock, DERIVED from the four buckets. It is never
// stored, so the total and its parts cannot drift apart.
func (p *InventoryPosition) OnHand() int64 {
	return p.available.Value() + p.reserved.Value() + p.allocated.Value() + p.quarantined.Value()
}

// IsEmpty reports whether the position holds no stock at all.
func (p *InventoryPosition) IsEmpty() bool { return p.OnHand() == 0 }

// IsQuarantined reports whether any stock is held.
func (p *InventoryPosition) IsQuarantined() bool { return !p.quarantined.IsZero() }

// Version returns the optimistic-lock token. Read-only in the domain;
// repositories alone advance it.
func (p *InventoryPosition) Version() uint64 { return p.version }

// CreatedBy returns the actor who opened the position.
func (p *InventoryPosition) CreatedBy() uuid.UUID { return p.createdBy }

// UpdatedBy returns the actor who last changed it.
func (p *InventoryPosition) UpdatedBy() uuid.UUID { return p.updatedBy }

// CreatedAt returns when the position was opened.
func (p *InventoryPosition) CreatedAt() time.Time { return p.createdAt }

// UpdatedAt returns when it was last changed.
func (p *InventoryPosition) UpdatedAt() time.Time { return p.updatedAt }
