package entity

import (
	"math/big"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type SKU string

func NewSKU(raw string) (SKU, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if len(v) == 0 || len(v) > 64 {
		return "", apperror.Validation("sku must be between 1 and 64 characters")
	}
	return SKU(v), nil
}
func (v SKU) String() string { return string(v) }

// ProductName is separate from a string so construction, reconstitution and
// later imports all apply the same meaningful-name rule.
type ProductName string

func NewProductName(raw string) (ProductName, error) {
	v := strings.TrimSpace(raw)
	if len(v) < 2 || len(v) > 255 {
		return "", apperror.Validation("product name must be between 2 and 255 characters")
	}
	return ProductName(v), nil
}
func (v ProductName) String() string { return string(v) }

type Barcode string

func NewBarcode(raw string) (Barcode, error) {
	v := strings.TrimSpace(raw)
	if len(v) == 0 || len(v) > 64 {
		return "", apperror.Validation("barcode must be between 1 and 64 characters")
	}
	return Barcode(v), nil
}
func (v Barcode) String() string { return string(v) }

// Decimal is an immutable exact rational. The public representation is a
// canonical fraction, never a binary float, so catalog conversions cannot
// accumulate rounding error into inventory quantities.
type Decimal struct{ value string }

func NewDecimal(raw string) (Decimal, error) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
	if !ok {
		return Decimal{}, apperror.Validation("must be a valid decimal")
	}
	return Decimal{value: r.RatString()}, nil
}
func MustDecimal(raw string) Decimal {
	v, err := NewDecimal(raw)
	if err != nil {
		panic(err)
	}
	return v
}
func (d Decimal) String() string { return d.value }
func (d Decimal) positive() bool { r, ok := new(big.Rat).SetString(d.value); return ok && r.Sign() > 0 }
func (d Decimal) nonNegative() bool {
	r, ok := new(big.Rat).SetString(d.value)
	return ok && r.Sign() >= 0
}

// isSet distinguishes a constructed Decimal from the struct zero value. The
// zero value carries an empty canonical string, which is not a number at all;
// every setter guards against it so an unconstructed value object can never
// slip past validation into the aggregate.
func (d Decimal) isSet() bool { return d.value != "" }

type ConversionFactor struct{ value Decimal }

func NewConversionFactor(raw string) (ConversionFactor, error) {
	v, e := NewDecimal(raw)
	if e != nil {
		return ConversionFactor{}, e
	}
	if !v.positive() {
		return ConversionFactor{}, apperror.Validation("conversion factor must be positive")
	}
	return ConversionFactor{v}, nil
}
func (v ConversionFactor) Decimal() Decimal { return v.value }

// IsValid reports whether the factor was produced by NewConversionFactor rather
// than being a struct zero value. AddUOM relies on this: a caller can hand the
// aggregate a ConversionFactor{} that never went through the constructor, and
// without this check that unvalidated, non-positive factor would enter the UOM
// set unnoticed.
func (v ConversionFactor) IsValid() bool { return v.value.isSet() && v.value.positive() }

// Canonical units are deliberate platform contracts: length is centimetres,
// mass kilograms and volume cubic metres. A future UOM module converts input
// to these units before Product is called.
type Length struct{ centimetres Decimal }

func NewLengthCentimetres(raw string) (Length, error) {
	v, e := NewDecimal(raw)
	if e != nil {
		return Length{}, e
	}
	if !v.positive() {
		return Length{}, apperror.Validation("length must be positive")
	}
	return Length{v}, nil
}
func (v Length) Centimetres() Decimal { return v.centimetres }

// IsSet reports whether the length was constructed and is positive, rejecting
// the struct zero value. Dimension composition depends on it.
func (v Length) IsSet() bool { return v.centimetres.isSet() && v.centimetres.positive() }

type Weight struct{ kilograms Decimal }

func NewWeightKilograms(raw string) (Weight, error) {
	v, e := NewDecimal(raw)
	if e != nil {
		return Weight{}, e
	}
	if !v.positive() {
		return Weight{}, apperror.Validation("weight must be positive")
	}
	return Weight{v}, nil
}
func (v Weight) Kilograms() Decimal { return v.kilograms }

// IsSet reports whether the weight was constructed and is positive.
func (v Weight) IsSet() bool { return v.kilograms.isSet() && v.kilograms.positive() }

type Volume struct{ cubicMetres Decimal }

func NewVolumeCubicMetres(raw string) (Volume, error) {
	v, e := NewDecimal(raw)
	if e != nil {
		return Volume{}, e
	}
	if !v.positive() {
		return Volume{}, apperror.Validation("volume must be positive")
	}
	return Volume{v}, nil
}
func (v Volume) CubicMetres() Decimal { return v.cubicMetres }

// IsSet reports whether the volume was constructed and is positive.
func (v Volume) IsSet() bool { return v.cubicMetres.isSet() && v.cubicMetres.positive() }

type Dimension struct{ width, height, length Length }

// NewDimension composes three lengths into a dimension, rejecting any that is a
// zero-value (unconstructed) Length. Every side must be a real, positive
// measurement — a box with a width and height but no length describes nothing
// physical, and slotting would divide by it.
func NewDimension(width, height, length Length) (Dimension, error) {
	if !width.IsSet() || !height.IsSet() || !length.IsSet() {
		return Dimension{}, apperror.Validation("dimension width, height and length must all be positive")
	}
	return Dimension{width, height, length}, nil
}
func (v Dimension) Width() Length  { return v.width }
func (v Dimension) Height() Length { return v.height }
func (v Dimension) Length() Length { return v.length }

// IsValid reports whether every side is a constructed, positive length. Used by
// SetMeasurements to reject a Dimension assembled from zero-value parts.
func (v Dimension) IsValid() bool { return v.width.IsSet() && v.height.IsSet() && v.length.IsSet() }

// ShelfLife distinguishes an unspecified shelf life from a defined zero-day
// shelf life. The latter is valid for products that expire on manufacture.
type ShelfLife struct {
	days    int
	defined bool
}

func NoShelfLife() ShelfLife { return ShelfLife{} }
func NewShelfLife(days int) (ShelfLife, error) {
	if days < 0 {
		return ShelfLife{}, apperror.Validation("shelf life must be zero or greater")
	}
	return ShelfLife{days: days, defined: true}, nil
}
func (v ShelfLife) IsDefined() bool { return v.defined }
func (v ShelfLife) Days() int       { return v.days }

type TrackingMethod string

const (
	TrackingNone   TrackingMethod = "NONE"
	TrackingLot    TrackingMethod = "LOT"
	TrackingSerial TrackingMethod = "SERIAL"
)

func (v TrackingMethod) Valid() bool {
	return v == TrackingNone || v == TrackingLot || v == TrackingSerial
}
func (v TrackingMethod) String() string { return string(v) }

type Status string

const (
	StatusDraft        Status = "DRAFT"
	StatusActive       Status = "ACTIVE"
	StatusDiscontinued Status = "DISCONTINUED"
)

func (v Status) Valid() bool    { return v == StatusDraft || v == StatusActive || v == StatusDiscontinued }
func (v Status) String() string { return string(v) }
