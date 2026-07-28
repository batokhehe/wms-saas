package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Customer is the AGGREGATE ROOT for customer master data — the structural
// sibling of Supplier. It has no exported fields and no setters: its status
// reaches INACTIVE only through Deactivate(), and its code and name can never be
// blank.
//
// # Invariants
//
//	CustomerCode is required and unique per company (uniqueness is a SET rule the
//	                                                 service enforces, backed by a
//	                                                 unique index; the aggregate
//	                                                 guarantees non-emptiness)
//	CustomerName is required
//	Status is ACTIVE or INACTIVE
//
// "A customer cannot be deleted while referenced by a sales order" is a
// cross-aggregate rule that references a module which does not exist yet. Its
// extension point is prepared in service/guards.go; there is no delete behaviour
// in this sprint.
type Customer struct {
	id        uuid.UUID
	companyID uuid.UUID

	code      CustomerCode
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

// NewCustomer is the FACTORY. It is the only way to create a customer, and it
// always produces one that is ACTIVE with version 1, raising CustomerCreated.
func NewCustomer(
	id, companyID uuid.UUID,
	code CustomerCode,
	name string,
	email Email,
	phone Phone,
	taxNumber TaxNumber,
	address Address,
	actor uuid.UUID,
	now time.Time,
) (*Customer, error) {
	if id == uuid.Nil || companyID == uuid.Nil || actor == uuid.Nil {
		return nil, apperror.Validation("customer, company and actor ids are required")
	}
	name, err := validateName(name)
	if err != nil {
		return nil, err
	}
	if code.String() == "" {
		return nil, apperror.Validation("customer code is required")
	}

	c := &Customer{
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

	c.record(EventCustomerCreated, actor, now, map[string]any{
		"code":   code.String(),
		"name":   name,
		"status": c.status.String(),
	})

	return c, nil
}

// Reconstitute restores stored state WITHOUT raising events. Loading a row is
// not a business event. It re-validates the persisted state so a corrupt row is
// refused.
func Reconstitute(
	id, companyID uuid.UUID,
	code CustomerCode,
	name string,
	email Email,
	phone Phone,
	taxNumber TaxNumber,
	address Address,
	status Status,
	version uint64,
	createdBy, updatedBy uuid.UUID,
	createdAt, updatedAt time.Time,
) (*Customer, error) {
	if version == 0 || id == uuid.Nil || companyID == uuid.Nil {
		return nil, apperror.Validation("invalid persisted customer state")
	}
	if !status.Valid() || code.String() == "" || name == "" {
		return nil, apperror.Validation("invalid persisted customer state")
	}

	return &Customer{
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
		return "", apperror.Validation("customer name must be between 2 and 255 characters")
	}
	return v, nil
}

// ---------------------------------------------------------------------------
// Behaviours
// ---------------------------------------------------------------------------

// Update replaces the mutable attributes: name and the optional contact and
// address details. The code is NOT updatable — it is an identifier printed on
// sales orders.
func (c *Customer) Update(
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
	c.name = validated
	c.email = email
	c.phone = phone
	c.taxNumber = taxNumber
	c.address = address
	c.touch(actor, now)
	c.record(EventCustomerUpdated, actor, now, map[string]any{"name": validated})
	return nil
}

// Activate makes a customer selectable for new sales orders. Idempotent — a
// no-op emits no event.
func (c *Customer) Activate(actor uuid.UUID, now time.Time) {
	if c.status == StatusActive {
		return
	}
	c.status = StatusActive
	c.touch(actor, now)
	c.record(EventCustomerActivated, actor, now, map[string]any{"status": c.status.String()})
}

// Deactivate retains a customer for history but removes it from selection for new
// orders. Idempotent for the same reason as Activate.
func (c *Customer) Deactivate(actor uuid.UUID, now time.Time) {
	if c.status == StatusInactive {
		return
	}
	c.status = StatusInactive
	c.touch(actor, now)
	c.record(EventCustomerDeactivated, actor, now, map[string]any{"status": c.status.String()})
}

// touch records who last changed the customer and when. Version is NOT advanced
// here: it is owned by the persistence layer.
func (c *Customer) touch(actor uuid.UUID, now time.Time) {
	c.updatedBy = actor
	c.updatedAt = now
}

// BelongsTo reports whether the customer is owned by the given tenant.
func (c *Customer) BelongsTo(companyID uuid.UUID) bool { return c.companyID == companyID }

// IsActive reports whether the customer is selectable for new orders.
func (c *Customer) IsActive() bool { return c.status == StatusActive }

// ---------------------------------------------------------------------------
// Accessors (read-only)
// ---------------------------------------------------------------------------

// ID returns the customer identity.
func (c *Customer) ID() uuid.UUID { return c.id }

// CompanyID returns the owning tenant.
func (c *Customer) CompanyID() uuid.UUID { return c.companyID }

// Code returns the customer code.
func (c *Customer) Code() CustomerCode { return c.code }

// Name returns the customer name.
func (c *Customer) Name() string { return c.name }

// Email returns the contact email, or its zero value when unset.
func (c *Customer) Email() Email { return c.email }

// Phone returns the contact phone, or its zero value when unset.
func (c *Customer) Phone() Phone { return c.phone }

// TaxNumber returns the tax number, or its zero value when unset.
func (c *Customer) TaxNumber() TaxNumber { return c.taxNumber }

// Address returns the postal address, or its zero value when unset.
func (c *Customer) Address() Address { return c.address }

// Status returns the lifecycle state.
func (c *Customer) Status() Status { return c.status }

// Version returns the optimistic-lock token. Read-only in the domain.
func (c *Customer) Version() uint64 { return c.version }

// CreatedBy returns the actor who created the customer.
func (c *Customer) CreatedBy() uuid.UUID { return c.createdBy }

// UpdatedBy returns the actor who last changed the customer.
func (c *Customer) UpdatedBy() uuid.UUID { return c.updatedBy }

// CreatedAt returns when the customer was created.
func (c *Customer) CreatedAt() time.Time { return c.createdAt }

// UpdatedAt returns when the customer was last changed.
func (c *Customer) UpdatedAt() time.Time { return c.updatedAt }
