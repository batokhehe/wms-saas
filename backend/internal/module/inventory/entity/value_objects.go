// Package entity holds the InventoryPosition aggregate, its value objects and
// its events.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror and
// the shared identity types, and nothing from any web or persistence framework.
//
// An InventoryPosition is the stock of ONE product, with ONE set of stock
// attributes, in ONE storage location. It owns every stock transition; nothing
// outside the aggregate moves a quantity.
package entity

import (
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Quantity
// ---------------------------------------------------------------------------

// Quantity is a strictly positive movement amount — the delta a behaviour
// applies. Zero is rejected because a zero-amount movement is a no-op that must
// not raise an event, and negative is a different behaviour (Receive vs Issue).
//
// Integers, not float64: a WMS adds and subtracts these thousands of times a
// day, and binary floating point accumulates error until an availability check
// passes when it should fail.
type Quantity struct{ value int64 }

// NewQuantity builds a movement amount.
func NewQuantity(v int64) (Quantity, error) {
	if v <= 0 {
		return Quantity{}, apperror.Validation("movement quantity must be greater than zero")
	}
	return Quantity{value: v}, nil
}

// MustQuantity is the panicking constructor, for tests and constants.
func MustQuantity(v int64) Quantity {
	q, err := NewQuantity(v)
	if err != nil {
		panic(err)
	}
	return q
}

// Value returns the underlying count.
func (q Quantity) Value() int64 { return q.value }

// ---------------------------------------------------------------------------
// QuantityBucket
// ---------------------------------------------------------------------------

// QuantityBucket is one non-negative balance inside a position: available,
// reserved, allocated or quarantined.
//
// # Why one type for four balances
//
// Every bucket obeys identical arithmetic: never negative, never overflowing,
// and only ever changed by adding or subtracting a positive Quantity. Expressing
// that once means the aggregate's transitions are a sequence of bucket moves it
// cannot get wrong, rather than four hand-written int64 updates that must each
// remember the same guards.
type QuantityBucket struct{ value int64 }

// NewQuantityBucket builds a bucket balance, rejecting a negative value.
func NewQuantityBucket(v int64) (QuantityBucket, error) {
	if v < 0 {
		return QuantityBucket{}, apperror.Validation("quantity bucket cannot be negative")
	}
	return QuantityBucket{value: v}, nil
}

// MustQuantityBucket is the panicking constructor, for tests.
func MustQuantityBucket(v int64) QuantityBucket {
	b, err := NewQuantityBucket(v)
	if err != nil {
		panic(err)
	}
	return b
}

// EmptyBucket is a zero balance.
func EmptyBucket() QuantityBucket { return QuantityBucket{} }

// Value returns the balance.
func (b QuantityBucket) Value() int64 { return b.value }

// IsZero reports whether the bucket is empty.
func (b QuantityBucket) IsZero() bool { return b.value == 0 }

// Covers reports whether the bucket holds at least amount.
func (b QuantityBucket) Covers(amount Quantity) bool { return b.value >= amount.value }

// Add returns the bucket increased by amount, guarding int64 overflow so a count
// can never silently wrap to a negative balance.
func (b QuantityBucket) Add(amount Quantity) (QuantityBucket, error) {
	if b.value > math.MaxInt64-amount.value {
		return QuantityBucket{}, apperror.Validation("quantity would overflow")
	}
	return QuantityBucket{value: b.value + amount.value}, nil
}

// Sub returns the bucket decreased by amount, refusing to go below zero.
func (b QuantityBucket) Sub(amount Quantity) (QuantityBucket, error) {
	if !b.Covers(amount) {
		return QuantityBucket{}, apperror.Conflict("insufficient quantity in bucket")
	}
	return QuantityBucket{value: b.value - amount.value}, nil
}

// ---------------------------------------------------------------------------
// TrackingType
// ---------------------------------------------------------------------------

// TrackingType is how the stock of a product is individuated.
type TrackingType string

const (
	// TrackingNone is a single fungible pool per product per location.
	TrackingNone TrackingType = "NONE"

	// TrackingLot is one position per batch, so expiry and recall are traceable.
	TrackingLot TrackingType = "LOT"

	// TrackingSerial is one position per unit; its on-hand never exceeds one.
	TrackingSerial TrackingType = "SERIAL"
)

// Valid reports whether the tracking type is a known value.
func (t TrackingType) Valid() bool {
	return t == TrackingNone || t == TrackingLot || t == TrackingSerial
}

// String renders the tracking type.
func (t TrackingType) String() string { return string(t) }

// ---------------------------------------------------------------------------
// LotNumber and SerialNumber
// ---------------------------------------------------------------------------

// LotNumber identifies a batch. Its zero value means "no lot".
type LotNumber struct{ value string }

// NewLotNumber builds a lot number, trimming and length-checking it.
func NewLotNumber(raw string) (LotNumber, error) {
	v := strings.TrimSpace(raw)
	if len(v) == 0 || len(v) > 64 {
		return LotNumber{}, apperror.Validation("lot number must be between 1 and 64 characters")
	}
	return LotNumber{value: v}, nil
}

// NoLotNumber is the explicit absence of a lot.
func NoLotNumber() LotNumber { return LotNumber{} }

// String renders the lot number.
func (l LotNumber) String() string { return l.value }

// IsZero reports whether no lot is set.
func (l LotNumber) IsZero() bool { return l.value == "" }

// SerialNumber identifies a single unit. Its zero value means "no serial".
type SerialNumber struct{ value string }

// NewSerialNumber builds a serial number, trimming and length-checking it.
func NewSerialNumber(raw string) (SerialNumber, error) {
	v := strings.TrimSpace(raw)
	if len(v) == 0 || len(v) > 128 {
		return SerialNumber{}, apperror.Validation("serial number must be between 1 and 128 characters")
	}
	return SerialNumber{value: v}, nil
}

// NoSerialNumber is the explicit absence of a serial.
func NoSerialNumber() SerialNumber { return SerialNumber{} }

// String renders the serial number.
func (s SerialNumber) String() string { return s.value }

// IsZero reports whether no serial is set.
func (s SerialNumber) IsZero() bool { return s.value == "" }

// ---------------------------------------------------------------------------
// StockAttributes
// ---------------------------------------------------------------------------

// StockAttributes is what individuates stock of the same product in the same
// place: how it is tracked, and the lot or serial that names it.
//
// It is a value object rather than three loose fields because the three are only
// ever valid TOGETHER — a LOT position without a lot number, or a NONE position
// carrying a serial, is not a partially-built position but a contradiction. The
// constructor is the single place that rule is enforced, so no aggregate method
// has to re-check it.
type StockAttributes struct {
	tracking TrackingType
	lot      LotNumber
	serial   SerialNumber
}

// NewStockAttributes composes and validates the tracking triple.
func NewStockAttributes(tracking TrackingType, lot LotNumber, serial SerialNumber) (StockAttributes, error) {
	if !tracking.Valid() {
		return StockAttributes{}, apperror.Validation("invalid tracking type")
	}
	switch tracking {
	case TrackingNone:
		if !lot.IsZero() || !serial.IsZero() {
			return StockAttributes{}, apperror.Validation("untracked stock must not carry a lot or serial number")
		}
	case TrackingLot:
		if lot.IsZero() {
			return StockAttributes{}, apperror.Validation("lot-tracked stock requires a lot number")
		}
		if !serial.IsZero() {
			return StockAttributes{}, apperror.Validation("lot-tracked stock must not carry a serial number")
		}
	case TrackingSerial:
		if serial.IsZero() {
			return StockAttributes{}, apperror.Validation("serial-tracked stock requires a serial number")
		}
		if !lot.IsZero() {
			return StockAttributes{}, apperror.Validation("serial-tracked stock must not carry a lot number")
		}
	}
	return StockAttributes{tracking: tracking, lot: lot, serial: serial}, nil
}

// UntrackedAttributes is the attribute set of a fungible pool.
func UntrackedAttributes() StockAttributes {
	return StockAttributes{tracking: TrackingNone}
}

// Tracking returns how the stock is individuated.
func (a StockAttributes) Tracking() TrackingType { return a.tracking }

// Lot returns the lot number, or its zero value.
func (a StockAttributes) Lot() LotNumber { return a.lot }

// Serial returns the serial number, or its zero value.
func (a StockAttributes) Serial() SerialNumber { return a.serial }

// HasLot reports whether a lot number is set.
func (a StockAttributes) HasLot() bool { return !a.lot.IsZero() }

// HasSerial reports whether a serial number is set.
func (a StockAttributes) HasSerial() bool { return !a.serial.IsZero() }

// IsSerialised reports whether this stock is tracked one unit at a time.
func (a StockAttributes) IsSerialised() bool { return a.tracking == TrackingSerial }

// Equals reports whether two attribute sets name the same stock.
func (a StockAttributes) Equals(other StockAttributes) bool {
	return a.tracking == other.tracking &&
		a.lot.value == other.lot.value &&
		a.serial.value == other.serial.value
}

// ---------------------------------------------------------------------------
// StockKey
// ---------------------------------------------------------------------------

// StockKey is the full address of a position: the tenant that owns it, the
// warehouse and location it sits in, the product it is of, and the attributes
// that individuate it.
//
// It is the aggregate's natural key. Making it one value object means "a
// position is identified by ALL of these together" is expressible in a signature
// — the repository's get-or-create takes a StockKey, not six loose arguments in
// an order a caller can transpose.
type StockKey struct {
	companyID   uuid.UUID
	warehouseID uuid.UUID
	locationID  uuid.UUID
	productID   uuid.UUID
	attributes  StockAttributes
}

// NewStockKey composes a stock key, requiring every identifier.
func NewStockKey(
	companyID, warehouseID, locationID, productID uuid.UUID,
	attributes StockAttributes,
) (StockKey, error) {
	if companyID == uuid.Nil || warehouseID == uuid.Nil ||
		locationID == uuid.Nil || productID == uuid.Nil {
		return StockKey{}, apperror.Validation("company, warehouse, location and product ids are required")
	}
	if !attributes.tracking.Valid() {
		return StockKey{}, apperror.Validation("invalid stock attributes")
	}
	return StockKey{
		companyID:   companyID,
		warehouseID: warehouseID,
		locationID:  locationID,
		productID:   productID,
		attributes:  attributes,
	}, nil
}

// CompanyID returns the owning tenant.
func (k StockKey) CompanyID() uuid.UUID { return k.companyID }

// WarehouseID returns the warehouse.
func (k StockKey) WarehouseID() uuid.UUID { return k.warehouseID }

// LocationID returns the storage location.
func (k StockKey) LocationID() uuid.UUID { return k.locationID }

// ProductID returns the product.
func (k StockKey) ProductID() uuid.UUID { return k.productID }

// Attributes returns the individuating attributes.
func (k StockKey) Attributes() StockAttributes { return k.attributes }

// Equals reports whether two keys address the same position.
func (k StockKey) Equals(other StockKey) bool {
	return k.companyID == other.companyID &&
		k.warehouseID == other.warehouseID &&
		k.locationID == other.locationID &&
		k.productID == other.productID &&
		k.attributes.Equals(other.attributes)
}

// WithLocation returns a copy of the key addressing a different location. It is
// how a transfer names its destination: same product, same attributes, new place.
func (k StockKey) WithLocation(warehouseID, locationID uuid.UUID) (StockKey, error) {
	return NewStockKey(k.companyID, warehouseID, locationID, k.productID, k.attributes)
}
