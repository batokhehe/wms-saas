// Package entity holds the Customer aggregate, its value objects and its events.
//
// LAYER RULE: entity imports nothing from this project except pkg/apperror and
// the shared identity/uuid types, and nothing from any web or persistence
// framework. It is the innermost layer.
//
// Customer is MASTER DATA owned by exactly one company — the structural sibling
// of Supplier. The value objects below validate themselves at construction, so a
// malformed code, email or tax number can never enter the aggregate.
package entity

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// CustomerCode is the operator-facing identifier of a customer, unique per
// company. Canonicalised to upper case so "CUS-01" and "cus-01" are the same
// customer to a human keying a sales order.
type CustomerCode struct{ value string }

// NewCustomerCode builds a code, trimming, upper-casing and length-checking it.
func NewCustomerCode(raw string) (CustomerCode, error) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if len(v) < 2 || len(v) > 32 {
		return CustomerCode{}, apperror.Validation("customer code must be between 2 and 32 characters")
	}
	return CustomerCode{value: v}, nil
}

// String renders the code.
func (v CustomerCode) String() string { return v.value }

// emailPattern is a deliberately permissive shape check: exactly one @, a
// non-empty local and domain part, and a dot in the domain.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Email is an optional contact address. Its zero value means "no email".
type Email struct{ value string }

// NewEmail builds a validated email. Callers that allow absence use NoEmail.
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

// NewPhone builds a phone number, trimming and length-checking it.
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

// Address is the customer's postal address as a single value object, composed of
// the street line plus the administrative fields. Bundling them means "a
// customer's address" is one thing set and read as a unit, while persistence
// still maps it to five columns. Every field is optional.
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

// Status is the lifecycle state of a customer.
type Status string

const (
	// StatusActive is a customer available for new sales orders.
	StatusActive Status = "ACTIVE"

	// StatusInactive is a customer retained for history but not selectable for
	// new orders.
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether the status is a known value.
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive }

// String renders the status.
func (s Status) String() string { return string(s) }
