package entity

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Status ----------

// Status is a location's operational state.
type Status string

const (
	// StatusActive may hold inventory and take part in putaway and picking.
	StatusActive Status = "ACTIVE"

	// StatusInactive is deliberately stood down — a rack awaiting
	// commissioning, an aisle no longer in the layout. Reversible.
	StatusInactive Status = "INACTIVE"

	// StatusLocked is an operational hold: damaged racking, a spill, a hold
	// pending investigation.
	//
	// Distinct from MAINTENANCE because the remedy differs. A lock is lifted
	// when the problem is resolved and someone decides it is safe; maintenance
	// ends when scheduled work finishes. Collapsing them would lose which of
	// those a location is waiting for.
	StatusLocked Status = "LOCKED"

	// StatusMaintenance is planned work — a rack being re-profiled, a scanner
	// beacon being replaced.
	StatusMaintenance Status = "MAINTENANCE"
)

// Valid reports whether s is a known status. Mirrors the CHECK constraint.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusInactive, StatusLocked, StatusMaintenance:
		return true
	default:
		return false
	}
}

// String renders the status.
func (s Status) String() string { return string(s) }

// ---------- LocationCode ----------

// codePattern constrains a location code.
//
// Hyphens are permitted because a code is normally the coordinate joined by
// them ("A-01-02-03"). Nothing else is, for the same reason the coordinate
// segments are narrow: this value is printed on labels and read over a radio.
var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,63}$`)

// LocationCode is the operator-facing identifier of a location.
//
// A value object rather than a string so an invalid code cannot exist: NewCode
// is the only way to obtain one, and it validates. A plain string field would
// let a caller assign "" and discover the problem at the database.
type LocationCode struct {
	value string
}

// NewLocationCode validates and canonicalises a code.
func NewLocationCode(raw string) (LocationCode, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))

	if value == "" {
		return LocationCode{}, apperror.NewValidation(apperror.FieldError{
			Field: "code", Rule: "required", Message: "code is required",
		}).WithOp("location.NewLocationCode")
	}

	if !codePattern.MatchString(value) {
		return LocationCode{}, apperror.NewValidation(apperror.FieldError{
			Field: "code", Rule: "format",
			Message: "code must be 1-64 characters: letters, digits and hyphens, starting with a letter or digit",
		}).WithOp("location.NewLocationCode")
	}

	return LocationCode{value: value}, nil
}

// DeriveCode builds a code from a coordinate.
//
// This is what makes a location's default identifier BE its physical address:
// an operator reading "A-01-02-03" off a label knows where to walk with nothing
// to look up.
//
// A caller may still supply an explicit code — a receiving dock is more useful
// labelled "DOCK-1" than "RECV" — which is why the factory accepts an override.
func DeriveCode(c Coordinate) (LocationCode, error) {
	return NewLocationCode(c.String())
}

// String renders the code.
func (c LocationCode) String() string { return c.value }

// IsZero reports whether the code is unset.
func (c LocationCode) IsZero() bool { return c.value == "" }

// ---------- Barcode ----------

// barcodePattern constrains a barcode.
//
// Wider than a location code because barcodes come from label printers and
// external systems: EAN, Code 128 and internal schemes all appear, and
// rejecting a valid printed label because of an underscore would make the field
// unusable.
var barcodePattern = regexp.MustCompile(`^[A-Za-z0-9._\-]{4,64}$`)

// Barcode is a scannable label attached to a location.
//
// Optional: many locations are never labelled. The zero value means "no
// barcode", which is why IsPresent exists rather than callers comparing to "".
type Barcode struct {
	value string
}

// NewBarcode validates a barcode. An empty value yields the zero Barcode
// without error — absence is legitimate, not a validation failure.
func NewBarcode(raw string) (Barcode, error) {
	value := strings.TrimSpace(raw)

	if value == "" {
		return Barcode{}, nil
	}

	if !barcodePattern.MatchString(value) {
		return Barcode{}, apperror.NewValidation(apperror.FieldError{
			Field: "barcode", Rule: "format",
			Message: "barcode must be 4-64 characters: letters, digits, dots, underscores and hyphens",
		}).WithOp("location.NewBarcode")
	}

	// Case is PRESERVED, unlike codes and coordinates. A barcode is a machine
	// token reproduced exactly by a scanner; upper-casing it would mean a label
	// printed with a lowercase check character no longer matches what is
	// stored.
	return Barcode{value: value}, nil
}

// String renders the barcode, or "" when absent.
func (b Barcode) String() string { return b.value }

// IsPresent reports whether a barcode has been assigned.
func (b Barcode) IsPresent() bool { return b.value != "" }

// ---------- Capacity ----------

// Quantity is a non-negative decimal measure.
//
// A *big.Rat rather than a float64. A WMS adds and subtracts these values
// thousands of times a day, and binary floating point accumulates error on
// every operation — the discrepancy surfaces as a capacity check that passes
// when it should fail, which is the class of bug nobody traces back to
// arithmetic. See docs/EntityConvention.md §6.
type Quantity struct {
	value *big.Rat
}

// NewQuantity parses a decimal string.
func NewQuantity(raw string, field string) (Quantity, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Quantity{}, nil
	}

	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return Quantity{}, apperror.NewValidation(apperror.FieldError{
			Field: field, Rule: "numeric",
			Message: field + " must be a decimal number",
		}).WithOp("location.NewQuantity")
	}

	if parsed.Sign() < 0 {
		return Quantity{}, apperror.NewValidation(apperror.FieldError{
			Field: field, Rule: "min",
			Message: field + " must not be negative",
		}).WithOp("location.NewQuantity")
	}

	return Quantity{value: parsed}, nil
}

// IsSet reports whether a value was supplied.
//
// Absence is meaningfully different from zero: an unmeasured bin accepts stock,
// a zero-capacity bin accepts none.
func (q Quantity) IsSet() bool { return q.value != nil }

// String renders the quantity with three decimal places, matching the column.
func (q Quantity) String() string {
	if q.value == nil {
		return ""
	}
	return q.value.FloatString(3)
}

// LessThan reports whether q is strictly less than other. An unset quantity is
// never less than anything — "unmeasured" is unbounded, not zero.
func (q Quantity) LessThan(other Quantity) bool {
	if !q.IsSet() || !other.IsSet() {
		return false
	}
	return q.value.Cmp(other.value) < 0
}

// Capacity is what a location can physically hold.
//
// All three limits are optional and independent. A pallet rack is constrained
// by pallet positions; a shelf by weight; a bulk floor area by volume. Forcing
// all three to be set would make most real locations unrepresentable.
type Capacity struct {
	maxWeight Quantity
	maxVolume Quantity
	maxPallet *int
}

// NewCapacity builds a capacity from optional limits.
func NewCapacity(maxWeight, maxVolume Quantity, maxPallet *int) (Capacity, error) {
	if maxPallet != nil && *maxPallet < 0 {
		return Capacity{}, apperror.NewValidation(apperror.FieldError{
			Field: "max_pallet", Rule: "min",
			Message: "max_pallet must not be negative",
		}).WithOp("location.NewCapacity")
	}

	return Capacity{maxWeight: maxWeight, maxVolume: maxVolume, maxPallet: maxPallet}, nil
}

// UnlimitedCapacity is a capacity with no limits declared.
func UnlimitedCapacity() Capacity { return Capacity{} }

// MaxWeight returns the weight limit.
func (c Capacity) MaxWeight() Quantity { return c.maxWeight }

// MaxVolume returns the volume limit.
func (c Capacity) MaxVolume() Quantity { return c.maxVolume }

// MaxPallet returns the pallet-position limit, or nil.
func (c Capacity) MaxPallet() *int {
	if c.maxPallet == nil {
		return nil
	}
	// A copy, so a caller cannot mutate the aggregate's limit through the
	// returned pointer.
	value := *c.maxPallet
	return &value
}

// IsUnlimited reports whether no limit at all is declared.
func (c Capacity) IsUnlimited() bool {
	return !c.maxWeight.IsSet() && !c.maxVolume.IsSet() && c.maxPallet == nil
}

// CanAccommodate reports whether this capacity is sufficient for a usage.
//
// # Why usage is passed IN
//
// The aggregate cannot see how full a location is — stock is another aggregate
// entirely, and one that loaded another would collapse the boundary that makes
// each independently consistent.
//
// So the SERVICE fetches the usage through CurrentCapacityProvider and hands it
// to the aggregate, which applies the rule. The rule stays in the domain; only
// the fact comes from outside. That is the shape every cross-aggregate
// invariant in this codebase takes.
//
// An unset limit is unbounded and always accommodates: reducing a bin from
// "500 kg" to "not measured" is a widening, not a narrowing.
func (c Capacity) CanAccommodate(usage Usage) bool {
	if c.maxWeight.IsSet() && c.maxWeight.LessThan(usage.Weight) {
		return false
	}
	if c.maxVolume.IsSet() && c.maxVolume.LessThan(usage.Volume) {
		return false
	}
	if c.maxPallet != nil && usage.Pallets != nil && *c.maxPallet < *usage.Pallets {
		return false
	}
	return true
}

// Usage is how much of a location is currently occupied.
//
// It is supplied by CurrentCapacityProvider — see service/guards.go. The zero
// value means "nothing stored", which is what the permissive default returns
// until the Inventory module exists.
type Usage struct {
	Weight  Quantity
	Volume  Quantity
	Pallets *int
}

// IsEmpty reports whether the location holds nothing measurable.
func (u Usage) IsEmpty() bool {
	return !u.Weight.IsSet() && !u.Volume.IsSet() && (u.Pallets == nil || *u.Pallets == 0)
}
