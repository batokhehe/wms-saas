// Package entity holds the Warehouse aggregate and its value objects.
//
// LAYER RULE: this package imports NOTHING from the rest of the project except
// pkg/apperror. In particular it does not embed shared/entity.BaseEntity and
// carries no GORM tags — see warehouse.go for why that departure from
// EntityConvention is what makes the aggregate an aggregate.
package entity

import (
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Status ----------

// Status is a warehouse's operational state.
//
// The transitions between these values are the aggregate's core invariant, and
// they are enforced in warehouse.go rather than by anything that reads this
// type. See docs/Warehouse.md §4 for the state machine.
type Status string

const (
	// StatusDraft is the creation state. A draft warehouse is registered but not
	// yet fit for operations: it may lack an address, a contact or zones.
	//
	// Its existence is why the table permits empty address and contact columns.
	// A warehouse is created before its details are known — a manager registers
	// the site, then fills it in — and forcing completeness at creation would
	// mean the record cannot exist until someone has every fact to hand.
	StatusDraft Status = "DRAFT"

	// StatusActive may receive and ship inventory. This is the ONLY status that
	// may.
	StatusActive Status = "ACTIVE"

	// StatusInactive is deliberately stood down — a seasonal site, a location
	// between leases. Reversible by its operator.
	StatusInactive Status = "INACTIVE"

	// StatusSuspended is an enforcement or safety hold — a failed audit, a fire
	// inspection, a compliance block.
	//
	// Semantically distinct from INACTIVE because the remedy differs: lifting a
	// suspension is a governance decision, reactivating is an operational one.
	// Collapsing them into one "disabled" flag would lose the reason, and the
	// reason is the first thing anyone asks.
	StatusSuspended Status = "SUSPENDED"
)

// Valid reports whether s is a known status. Mirrors the CHECK constraint.
func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusActive, StatusInactive, StatusSuspended:
		return true
	default:
		return false
	}
}

// String renders the status.
func (s Status) String() string { return string(s) }

// ---------- Type ----------

// Type is a warehouse's role in the distribution network.
//
// It is descriptive rather than behavioural today: nothing in this sprint
// branches on it. It is modelled now because it is part of the ubiquitous
// language operators already use, and because future modules will branch on it
// — a TRANSIT site holds nothing overnight, a CONSIGNMENT site holds stock the
// company does not own.
type Type string

const (
	// TypeMain is a primary distribution centre.
	TypeMain Type = "MAIN"
	// TypeBranch is a regional site fed by a main warehouse.
	TypeBranch Type = "BRANCH"
	// TypeTransit is a cross-dock: stock passes through rather than resting.
	TypeTransit Type = "TRANSIT"
	// TypeConsignment holds stock owned by a third party.
	TypeConsignment Type = "CONSIGNMENT"
)

// Valid reports whether t is a known type. Mirrors the CHECK constraint.
func (t Type) Valid() bool {
	switch t {
	case TypeMain, TypeBranch, TypeTransit, TypeConsignment:
		return true
	default:
		return false
	}
}

// String renders the type.
func (t Type) String() string { return string(t) }

// ---------- Code ----------

// codePattern constrains a warehouse code to letters, digits and hyphens.
//
// Deliberately narrow. The code is printed on labels, embedded in filenames and
// read aloud over a radio, so permitting spaces or punctuation would mean
// escaping it correctly in every one of those places forever.
var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,31}$`)

// Code is a warehouse's operator-facing identifier.
//
// It is a value object rather than a string so that an invalid code cannot
// exist: NewCode is the only way to obtain one, and it validates. A plain
// string field would let a caller assign "" and discover the problem at the
// database.
type Code struct {
	value string
}

// NewCode validates and canonicalises a warehouse code.
func NewCode(raw string) (Code, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))

	if value == "" {
		return Code{}, apperror.NewValidation(apperror.FieldError{
			Field:   "code",
			Rule:    "required",
			Message: "code is required",
		}).WithOp("warehouse.NewCode")
	}

	if !codePattern.MatchString(value) {
		return Code{}, apperror.NewValidation(apperror.FieldError{
			Field:   "code",
			Rule:    "format",
			Message: "code must be 2-32 characters: letters, digits and hyphens, starting with a letter or digit",
		}).WithOp("warehouse.NewCode")
	}

	return Code{value: value}, nil
}

// String renders the code.
func (c Code) String() string { return c.value }

// IsZero reports whether the code is unset.
func (c Code) IsZero() bool { return c.value == "" }

// ---------- Address ----------

// Address is where a warehouse physically is.
//
// A value object rather than a string field because "does this warehouse have
// an address?" is a question the activation rule asks, and a method answers it
// more honestly than a length check scattered across the aggregate.
//
// It is a single free-text line rather than structured components. Structured
// addresses are seductive and wrong at this stage: the correct decomposition
// differs by country, and guessing it now would force a migration the moment
// the first non-domestic customer arrives. Structure it when a feature needs to
// query by it.
type Address struct {
	value string
}

// NewAddress builds an address. An empty address is legitimate — a DRAFT
// warehouse may not have one yet — so this does not reject it. Activation is
// where completeness is enforced.
func NewAddress(raw string) (Address, error) {
	value := strings.TrimSpace(raw)

	const maxAddressLength = 2000
	if len(value) > maxAddressLength {
		return Address{}, apperror.NewValidation(apperror.FieldError{
			Field:   "address",
			Rule:    "max",
			Message: "address must be at most 2000 characters",
		}).WithOp("warehouse.NewAddress")
	}

	return Address{value: value}, nil
}

// String renders the address.
func (a Address) String() string { return a.value }

// IsPresent reports whether an address has been supplied. The activation rule
// depends on this.
func (a Address) IsPresent() bool { return a.value != "" }

// ---------- Contact ----------

// Contact is the person responsible for a warehouse.
//
// Name and phone travel together as one value object because they are
// meaningless apart: a phone number with nobody attached cannot be acted on,
// and a name with no number cannot be reached. Modelling them as two
// independent string fields would permit exactly those half-states.
type Contact struct {
	name  string
	phone string
}

// NewContact builds a contact.
//
// Like Address, an empty contact is legitimate for a DRAFT warehouse. What is
// NOT legitimate is a half-contact, which this rejects.
func NewContact(rawName, rawPhone string) (Contact, error) {
	name := strings.TrimSpace(rawName)
	phone := strings.TrimSpace(rawPhone)

	var fields []apperror.FieldError

	const maxNameLength, maxPhoneLength = 255, 32
	if len(name) > maxNameLength {
		fields = append(fields, apperror.FieldError{
			Field: "contact_name", Rule: "max",
			Message: "contact_name must be at most 255 characters",
		})
	}
	if len(phone) > maxPhoneLength {
		fields = append(fields, apperror.FieldError{
			Field: "contact_phone", Rule: "max",
			Message: "contact_phone must be at most 32 characters",
		})
	}

	// The half-state rule. A name with no number gives an operator someone to
	// blame and no way to call them.
	if (name == "") != (phone == "") {
		fields = append(fields, apperror.FieldError{
			Field:   "contact",
			Rule:    "incomplete",
			Message: "contact_name and contact_phone must be supplied together",
		})
	}

	if len(fields) > 0 {
		return Contact{}, apperror.NewValidation(fields...).WithOp("warehouse.NewContact")
	}

	return Contact{name: name, phone: phone}, nil
}

// Name returns the contact's name.
func (c Contact) Name() string { return c.name }

// Phone returns the contact's phone number.
func (c Contact) Phone() string { return c.phone }

// IsPresent reports whether a contact has been supplied.
func (c Contact) IsPresent() bool { return c.name != "" && c.phone != "" }

// ---------- Zones ----------

// ZoneKind names an operational zone role.
//
// A warehouse assigns one zone per kind: where goods arrive, where they leave,
// and where they rest in between. These are the three points every inbound and
// outbound flow touches.
type ZoneKind string

const (
	ZoneReceiving ZoneKind = "RECEIVING"
	ZoneShipping  ZoneKind = "SHIPPING"
	ZoneStaging   ZoneKind = "STAGING"
)

// Valid reports whether k is a known zone kind.
func (k ZoneKind) Valid() bool {
	switch k {
	case ZoneReceiving, ZoneShipping, ZoneStaging:
		return true
	default:
		return false
	}
}

// String renders the zone kind.
func (k ZoneKind) String() string { return string(k) }

// Zones records a warehouse's default operational zone assignments.
//
// # Why the ids are unvalidated UUIDs
//
// These will reference the future Location aggregate, which does not exist. The
// aggregate therefore cannot check that a zone id is real, only that it is
// well-formed and belongs to a known kind.
//
// That gap is deliberate and bounded: the Location sprint adds a repository
// lookup at the service layer (see service.ZoneVerifier), not a change to this
// type. Modelling zones as a value object now means that change touches one
// collaborator rather than every method that reads a zone.
type Zones struct {
	receiving *uuid.UUID
	shipping  *uuid.UUID
	staging   *uuid.UUID
}

// NewZones builds a zone assignment from optional ids.
func NewZones(receiving, shipping, staging *uuid.UUID) Zones {
	return Zones{receiving: receiving, shipping: shipping, staging: staging}
}

// Receiving returns the default receiving zone, or nil.
func (z Zones) Receiving() *uuid.UUID { return z.receiving }

// Shipping returns the default shipping zone, or nil.
func (z Zones) Shipping() *uuid.UUID { return z.shipping }

// Staging returns the default staging zone, or nil.
func (z Zones) Staging() *uuid.UUID { return z.staging }

// Assign returns a copy with one zone set.
//
// Value objects are immutable: assigning returns a new Zones rather than
// mutating this one. That is what lets the aggregate hold a Zones by value
// without any caller being able to reach in and change it.
func (z Zones) Assign(kind ZoneKind, zoneID uuid.UUID) Zones {
	switch kind {
	case ZoneReceiving:
		z.receiving = &zoneID
	case ZoneShipping:
		z.shipping = &zoneID
	case ZoneStaging:
		z.staging = &zoneID
	}
	return z
}

// Get returns the zone assigned to a kind, or nil.
func (z Zones) Get(kind ZoneKind) *uuid.UUID {
	switch kind {
	case ZoneReceiving:
		return z.receiving
	case ZoneShipping:
		return z.shipping
	case ZoneStaging:
		return z.staging
	default:
		return nil
	}
}

// HasAny reports whether at least one operational zone is assigned.
//
// This is the activation rule's zone requirement. "At least one" rather than
// "all three" because the requirement differs by warehouse type: a TRANSIT
// cross-dock legitimately has no staging area, and a CONSIGNMENT site may only
// ever receive. Demanding all three would make those configurations
// unactivatable.
func (z Zones) HasAny() bool {
	return z.receiving != nil || z.shipping != nil || z.staging != nil
}

// Count reports how many zones are assigned.
func (z Zones) Count() int {
	count := 0
	for _, id := range []*uuid.UUID{z.receiving, z.shipping, z.staging} {
		if id != nil {
			count++
		}
	}
	return count
}
