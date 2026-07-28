// Package entity holds the Inventory aggregate, its value objects and its
// events.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror and
// the shared identity/uuid types, and nothing from any web or persistence
// framework. It is the innermost layer.
//
// Inventory is the current state of ONE Product inside ONE Storage Location. It
// is not a stock ledger row — it owns every stock transition, and nothing
// outside the aggregate may mutate a quantity. Every behaviour on the aggregate
// preserves the invariants documented in inventory.go.
package entity

import (
	"math"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------------------------------------------------------------------------
// Quantities
// ---------------------------------------------------------------------------
//
// Quantities are exact non-negative integers (counts of base units). Integers,
// not float64: a WMS adds and subtracts these thousands of times a day, and
// binary floating point accumulates error on every operation until an
// availability check passes when it should fail. Every arithmetic helper guards
// against negativity and against int64 overflow, so no behaviour can silently
// wrap a count.
//
// The three storage roles are DISTINCT types — InventoryQuantity (on hand),
// ReservedQuantity, AvailableQuantity — so a caller cannot pass a reserved count
// where an on-hand count is expected. Movement amounts use the separate
// Quantity, which is strictly POSITIVE: a zero-amount movement is a no-op that
// must not raise an event, and a negative one is a different behaviour
// (Increase vs Decrease).

// addInt64 adds two counts, rejecting overflow. b is expected non-negative.
func addInt64(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, apperror.Validation("quantity would overflow")
	}
	return a + b, nil
}

// Quantity is a positive movement amount — the delta a behaviour applies.
type Quantity struct{ value int64 }

// NewQuantity builds a movement amount. It must be strictly positive.
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

// InventoryQuantity is an on-hand count. Non-negative.
type InventoryQuantity struct{ value int64 }

// NewInventoryQuantity builds an on-hand count, rejecting a negative value.
func NewInventoryQuantity(v int64) (InventoryQuantity, error) {
	if v < 0 {
		return InventoryQuantity{}, apperror.Validation("on-hand quantity cannot be negative")
	}
	return InventoryQuantity{value: v}, nil
}

// MustInventoryQuantity is the panicking constructor, for tests.
func MustInventoryQuantity(v int64) InventoryQuantity {
	q, err := NewInventoryQuantity(v)
	if err != nil {
		panic(err)
	}
	return q
}

// Value returns the underlying count.
func (q InventoryQuantity) Value() int64 { return q.value }

// IsZero reports whether the count is zero.
func (q InventoryQuantity) IsZero() bool { return q.value == 0 }

// add returns on-hand increased by a movement amount, guarding overflow.
func (q InventoryQuantity) add(amount Quantity) (InventoryQuantity, error) {
	sum, err := addInt64(q.value, amount.value)
	if err != nil {
		return InventoryQuantity{}, err
	}
	return InventoryQuantity{value: sum}, nil
}

// sub returns on-hand decreased by a movement amount, rejecting a result below
// zero.
func (q InventoryQuantity) sub(amount Quantity) (InventoryQuantity, error) {
	if amount.value > q.value {
		return InventoryQuantity{}, apperror.Conflict("insufficient on-hand quantity")
	}
	return InventoryQuantity{value: q.value - amount.value}, nil
}

// coversReserved reports whether this on-hand count is at least the reserved
// count — the OnHand >= Reserved invariant.
func (q InventoryQuantity) coversReserved(r ReservedQuantity) bool {
	return q.value >= r.value
}

// ReservedQuantity is a reserved (promised-but-not-yet-shipped) count.
// Non-negative.
type ReservedQuantity struct{ value int64 }

// NewReservedQuantity builds a reserved count, rejecting a negative value.
func NewReservedQuantity(v int64) (ReservedQuantity, error) {
	if v < 0 {
		return ReservedQuantity{}, apperror.Validation("reserved quantity cannot be negative")
	}
	return ReservedQuantity{value: v}, nil
}

// Value returns the underlying count.
func (q ReservedQuantity) Value() int64 { return q.value }

// IsZero reports whether nothing is reserved.
func (q ReservedQuantity) IsZero() bool { return q.value == 0 }

// add returns reserved increased by a movement amount, guarding overflow.
func (q ReservedQuantity) add(amount Quantity) (ReservedQuantity, error) {
	sum, err := addInt64(q.value, amount.value)
	if err != nil {
		return ReservedQuantity{}, err
	}
	return ReservedQuantity{value: sum}, nil
}

// sub returns reserved decreased by a movement amount, rejecting a result below
// zero.
func (q ReservedQuantity) sub(amount Quantity) (ReservedQuantity, error) {
	if amount.value > q.value {
		return ReservedQuantity{}, apperror.Conflict("cannot release more than is reserved")
	}
	return ReservedQuantity{value: q.value - amount.value}, nil
}

// AvailableQuantity is on-hand minus reserved — what may be freshly reserved,
// picked or transferred. Non-negative by construction, since the aggregate
// upholds OnHand >= Reserved.
type AvailableQuantity struct{ value int64 }

// Value returns the underlying count.
func (q AvailableQuantity) Value() int64 { return q.value }

// IsZero reports whether nothing is available.
func (q AvailableQuantity) IsZero() bool { return q.value == 0 }

// covers reports whether at least amount is available.
func (q AvailableQuantity) covers(amount Quantity) bool { return q.value >= amount.value }

// available computes AvailableQuantity from an on-hand and a reserved count. It
// is the single place the derivation lives.
func available(onHand InventoryQuantity, reserved ReservedQuantity) AvailableQuantity {
	return AvailableQuantity{value: onHand.value - reserved.value}
}

// ---------------------------------------------------------------------------
// TrackingType
// ---------------------------------------------------------------------------

// TrackingType is how the stock of a product is individuated.
//
//   - NONE:   a single fungible pool per product per location.
//   - LOT:    one record per lot (batch), so expiry and recall are traceable.
//   - SERIAL: one record per serial, quantity always exactly one.
type TrackingType string

const (
	TrackingNone   TrackingType = "NONE"
	TrackingLot    TrackingType = "LOT"
	TrackingSerial TrackingType = "SERIAL"
)

// Valid reports whether the tracking type is a known value.
func (t TrackingType) Valid() bool {
	return t == TrackingNone || t == TrackingLot || t == TrackingSerial
}

// String renders the tracking type.
func (t TrackingType) String() string { return string(t) }

// RequiresLot reports whether a lot number is mandatory for this type.
func (t TrackingType) RequiresLot() bool { return t == TrackingLot }

// RequiresSerial reports whether a serial number is mandatory for this type.
func (t TrackingType) RequiresSerial() bool { return t == TrackingSerial }

// ---------------------------------------------------------------------------
// InventoryStatus
// ---------------------------------------------------------------------------

// InventoryStatus is the availability state of an inventory record. The state
// machine is deliberately minimal: ACTIVE <-> LOCKED, no other transitions.
type InventoryStatus string

const (
	// StatusActive is the normal state: every behaviour is permitted.
	StatusActive InventoryStatus = "ACTIVE"

	// StatusLocked freezes the record — no quantity may move — while a
	// governance hold, an investigation or a count is in progress.
	StatusLocked InventoryStatus = "LOCKED"
)

// Valid reports whether the status is a known value.
func (s InventoryStatus) Valid() bool {
	return s == StatusActive || s == StatusLocked
}

// String renders the status.
func (s InventoryStatus) String() string { return string(s) }

// ---------------------------------------------------------------------------
// LotNumber and SerialNumber
// ---------------------------------------------------------------------------

// LotNumber identifies a batch. Its zero value means "no lot", which is the
// correct state for NONE and SERIAL tracking.
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
