package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Supplier is the AGGREGATE ROOT for supplier master data. It has no exported
// fields and no setters: its status reaches INACTIVE only through Deactivate(),
// and its code and name can never be blank, because every path that sets them
// goes through a value object or the factory's validation.
//
// # Invariants
//
//	SupplierCode is required and unique per company (uniqueness is a SET rule the
//	                                                 service enforces, backed by a
//	                                                 unique index; the aggregate
//	                                                 guarantees non-emptiness)
//	SupplierName is required
//	Status is ACTIVE or INACTIVE
//
// "A supplier cannot be deleted while referenced by a purchase order" is a
// cross-aggregate rule that references a module which does not exist yet. Its
// extension point is prepared in service/guards.go; there is no delete behaviour
// in this sprint.
type Supplier struct {
	id        uuid.UUID
	companyID uuid.UUID

	code      SupplierCode
	name      string
	email     Email
	phone     Phone
	taxNumber TaxNumber
	address   Address

	status Status

	version uint64

	createdBy uuid.UUID
	updatedBy uuid.UUID
	createdAt time.Time
	updatedAt time.Time

	events []Event
}

// NewSupplier is the FACTORY. It is the only way to create a supplier, and it
// always produces one that is ACTIVE with version 1, raising SupplierCreated.
func NewSupplier(
	id, companyID uuid.UUID,
	code SupplierCode,
	name string,
	email Email,
	phone Phone,
	taxNumber TaxNumber,
	address Address,
	actor uuid.UUID,
	now time.Time,
) (*Supplier, error) {
	if id == uuid.Nil || companyID == uuid.Nil || actor == uuid.Nil {
		return nil, apperror.Validation("supplier, company and actor ids are required")
	}
	name, err := validateName(name)
	if err != nil {
		return nil, err
	}
	if code.String() == "" {
		return nil, apperror.Validation("supplier code is required")
	}

	s := &Supplier{
		id:        id,
		companyID: companyID,
		code:      code,
		name:      name,
		email:     email,
		phone:     phone,
		taxNumber: taxNumber,
		address:   address,
		status:    StatusActive,
		version:   1,
		createdBy: actor,
		updatedBy: actor,
		createdAt: now,
		updatedAt: now,
	}

	s.record(EventSupplierCreated, actor, now, map[string]any{
		"code":   code.String(),
		"name":   name,
		"status": s.status.String(),
	})

	return s, nil
}

// Reconstitute restores stored state WITHOUT raising events. Loading a row is
// not a business event: the factory would raise SupplierCreated on every read.
// It re-validates the persisted state so a corrupt row is refused.
func Reconstitute(
	id, companyID uuid.UUID,
	code SupplierCode,
	name string,
	email Email,
	phone Phone,
	taxNumber TaxNumber,
	address Address,
	status Status,
	version uint64,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*Supplier, error) {
	if version == 0 || id == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("invalid persisted supplier state")
	}
	if !status.Valid() || code.String() == "" || name == "" {
		return nil, apperror.Validation("invalid persisted supplier state")
	}

	return &Supplier{
		id:        id,
		companyID: companyID,
		code:      code,
		name:      name,
		email:     email,
		phone:     phone,
		taxNumber: taxNumber,
		address:   address,
		status:    status,
		version:   version,
		createdBy: createdBy,
		updatedBy: updatedBy,
		createdAt: createdAt,
		updatedAt: updatedAt,
	}, nil
}

// validateName enforces the required-name invariant, returning the trimmed name.
func validateName(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if len(v) < 2 || len(v) > 255 {
		return "", apperror.Validation("supplier name must be between 2 and 255 characters")
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Behaviours
// ---------------------------------------------------------------------------

// Update replaces the mutable attributes: name and the optional contact and
// address details. The code is NOT updatable — it is an identifier printed on
// purchase orders, so changing it would invalidate documents the system does not
// control.
func (s *Supplier) Update(
	name string,
	email Email,
	phone Phone,
	taxNumber TaxNumber,
	address Address,
	actor uuid.UUID,
	now time.Time,
) error {
	validated, err := validateName(name)
	if err != nil {
		return err
	}
	s.name = validated
	s.email = email
	s.phone = phone
	s.taxNumber = taxNumber
	s.address = address
	s.touch(actor, now)
	s.record(EventSupplierUpdated, actor, now, map[string]any{"name": validated})
	return nil
}

// Activate makes a supplier selectable for new purchase orders. It is idempotent
// — activating an already-active supplier is a no-op with no event, so a client
// retrying a request does not produce a spurious transition.
func (s *Supplier) Activate(actor uuid.UUID, now time.Time) {
	if s.status == StatusActive {
		return
	}
	s.status = StatusActive
	s.touch(actor, now)
	s.record(EventSupplierActivated, actor, now, map[string]any{"status": s.status.String()})
}

// Deactivate retains a supplier for history but removes it from selection for
// new orders. It is idempotent for the same reason as Activate.
func (s *Supplier) Deactivate(actor uuid.UUID, now time.Time) {
	if s.status == StatusInactive {
		return
	}
	s.status = StatusInactive
	s.touch(actor, now)
	s.record(EventSupplierDeactivated, actor, now, map[string]any{"status": s.status.String()})
}

// touch records who last changed the supplier and when. Version is NOT advanced
// here: it is owned by the persistence layer, and the aggregate exposes it
// read-only.
func (s *Supplier) touch(actor uuid.UUID, now time.Time) {
	s.updatedBy = actor
	s.updatedAt = now
}

// BelongsTo reports whether the supplier is owned by the given tenant. It backs
// the defence-in-depth check in the service layer.
func (s *Supplier) BelongsTo(companyID uuid.UUID) bool { return s.companyID == companyID }

// IsActive reports whether the supplier is selectable for new orders.
func (s *Supplier) IsActive() bool { return s.status == StatusActive }

// ---------------------------------------------------------------------------
// Accessors (read-only)
// ---------------------------------------------------------------------------

// ID returns the supplier identity.
func (s *Supplier) ID() uuid.UUID { return s.id }

// CompanyID returns the owning tenant.
func (s *Supplier) CompanyID() uuid.UUID { return s.companyID }

// Code returns the supplier code.
func (s *Supplier) Code() SupplierCode { return s.code }

// Name returns the supplier name.
func (s *Supplier) Name() string { return s.name }

// Email returns the contact email, or its zero value when unset.
func (s *Supplier) Email() Email { return s.email }

// Phone returns the contact phone, or its zero value when unset.
func (s *Supplier) Phone() Phone { return s.phone }

// TaxNumber returns the tax number, or its zero value when unset.
func (s *Supplier) TaxNumber() TaxNumber { return s.taxNumber }

// Address returns the postal address, or its zero value when unset.
func (s *Supplier) Address() Address { return s.address }

// Status returns the lifecycle state.
func (s *Supplier) Status() Status { return s.status }

// Version returns the optimistic-lock token. It is read-only in the domain;
// repositories are the sole owner of its advancement.
func (s *Supplier) Version() uint64 { return s.version }

// CreatedBy returns the actor who created the supplier.
func (s *Supplier) CreatedBy() uuid.UUID { return s.createdBy }

// UpdatedBy returns the actor who last changed the supplier.
func (s *Supplier) UpdatedBy() uuid.UUID { return s.updatedBy }

// CreatedAt returns when the supplier was created.
func (s *Supplier) CreatedAt() time.Time { return s.createdAt }

// UpdatedAt returns when the supplier was last changed.
func (s *Supplier) UpdatedAt() time.Time { return s.updatedAt }
