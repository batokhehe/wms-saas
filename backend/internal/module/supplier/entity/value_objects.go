// Package entity holds the Supplier aggregate, its value objects and its events.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror and
// the shared identity/uuid types, and nothing from any web or persistence
// framework. It is the innermost layer.
//
// Supplier is MASTER DATA owned by exactly one company. The value objects below
// validate themselves at construction, so a malformed code, email or tax number
// can never enter the aggregate — every setter goes through them.
package entity

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// SupplierCode is the operator-facing identifier of a supplier, unique per
// company. Canonicalised to upper case so "SUP-01" and "sup-01" are the same
// supplier to a human keying a purchase order.
type SupplierCode struct{ value string }

// NewSupplierCode builds a code, trimming, upper-casing and length-checking it.
func NewSupplierCode(raw string) (SupplierCode, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if len(v) < 2 || len(v) > 32 {
		return SupplierCode{}, apperror.Validation("supplier code must be between 2 and 32 characters")
	}
	return SupplierCode{value: v}, nil
}

// String renders the code.
func (v SupplierCode) String() string { return v.value }

// emailPattern is a deliberately permissive shape check: exactly one @, a
// non-empty local and domain part, and a dot in the domain. Full RFC 5322
// validation belongs to the mail server that will actually deliver, not here.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Email is an optional contact address. Its zero value means "no email", which
// is valid — a supplier may be onboarded before an address is known.
type Email struct{ value string }

// NewEmail builds a validated email. It requires a non-empty, well-formed value;
// callers that allow absence use NoEmail instead.
func NewEmail(raw string) (Email, error) {
	v := strings.TrimSpace(raw)
	if len(v) == 0 || len(v) > 255 || !emailPattern.MatchString(v) {
		return Email{}, apperror.Validation("email must be a valid address")
	}
	return Email{value: strings.ToLower(v)}, nil
}

// NoEmail is the explicit absence of an email.
func NoEmail() Email { return Email{} }

// String renders the email.
func (v Email) String() string { return v.value }

// IsZero reports whether no email is set.
func (v Email) IsZero() bool { return v.value == "" }

// Phone is an optional contact number. Its zero value means "no phone".
type Phone struct{ value string }

// NewPhone builds a phone number, trimming and length-checking it. It does not
// enforce a national format — suppliers are global, and a rigid pattern would
// reject legitimate numbers.
func NewPhone(raw string) (Phone, error) {
	v := strings.TrimSpace(raw)
	if len(v) < 3 || len(v) > 32 {
		return Phone{}, apperror.Validation("phone must be between 3 and 32 characters")
	}
	return Phone{value: v}, nil
}

// NoPhone is the explicit absence of a phone.
func NoPhone() Phone { return Phone{} }

// String renders the phone.
func (v Phone) String() string { return v.value }

// IsZero reports whether no phone is set.
func (v Phone) IsZero() bool { return v.value == "" }

// TaxNumber is an optional registration identifier (VAT/NPWP/EIN). Its zero
// value means "no tax number".
type TaxNumber struct{ value string }

// NewTaxNumber builds a tax number, trimming and length-checking it.
func NewTaxNumber(raw string) (TaxNumber, error) {
	v := strings.TrimSpace(raw)
	if len(v) == 0 || len(v) > 64 {
		return TaxNumber{}, apperror.Validation("tax number must be between 1 and 64 characters")
	}
	return TaxNumber{value: v}, nil
}

// NoTaxNumber is the explicit absence of a tax number.
func NoTaxNumber() TaxNumber { return TaxNumber{} }

// String renders the tax number.
func (v TaxNumber) String() string { return v.value }

// IsZero reports whether no tax number is set.
func (v TaxNumber) IsZero() bool { return v.value == "" }

// Address is the supplier's postal address as a single value object, composed of
// the street line plus the administrative fields. Bundling them here — rather
// than scattering five strings through the aggregate — means "a supplier's
// address" is one thing that is set and read as a unit, while persistence still
// maps it to five columns.
//
// Every field is optional: a supplier may be created before its address is
// captured. The constructor validates only length, never presence.
type Address struct {
	street     string
	city       string
	province   string
	country    string
	postalCode string
}

// NewAddress composes an address, trimming and length-checking each field.
func NewAddress(street, city, province, country, postalCode string) (Address, error) {
	fields := []struct {
		name  string
		value *string
		max   int
	}{
		{"street", &street, 500},
		{"city", &city, 128},
		{"province", &province, 128},
		{"country", &country, 128},
		{"postal code", &postalCode, 16},
	}
	for _, f := range fields {
		*f.value = strings.TrimSpace(*f.value)
		if len(*f.value) > f.max {
			return Address{}, apperror.Validation(fmt.Sprintf("%s must be at most %d characters", f.name, f.max))
		}
	}
	return Address{street: street, city: city, province: province, country: country, postalCode: postalCode}, nil
}

// EmptyAddress is the zero address.
func EmptyAddress() Address { return Address{} }

// Street returns the street line.
func (a Address) Street() string { return a.street }

// City returns the city.
func (a Address) City() string { return a.city }

// Province returns the province or state.
func (a Address) Province() string { return a.province }

// Country returns the country.
func (a Address) Country() string { return a.country }

// PostalCode returns the postal code.
func (a Address) PostalCode() string { return a.postalCode }

// IsZero reports whether no address field is set.
func (a Address) IsZero() bool {
	return a.street == "" && a.city == "" && a.province == "" && a.country == "" && a.postalCode == ""
}

// Status is the lifecycle state of a supplier.
type Status string

const (
	// StatusActive is a supplier available for new purchase orders.
	StatusActive Status = "ACTIVE"

	// StatusInactive is a supplier retained for history but not selectable for
	// new orders.
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether the status is a known value.
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive }

// String renders the status.
func (s Status) String() string { return string(s) }
