package entity

import (
	"strings"
	"time"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

func (s Status) Valid() bool    { return s == StatusActive || s == StatusInactive }
func (s Status) String() string { return string(s) }

type Category struct {
	id, companyID           uuid.UUID
	code, name, description string
	status                  Status
	createdAt, updatedAt    time.Time
	deletedAt               *time.Time
	version                 uint64
}

func New(id, companyID uuid.UUID, code, name, description string, now time.Time) (*Category, error) {
	c := &Category{
		id: id, companyID: companyID,
		code:        strings.ToUpper(strings.TrimSpace(code)),
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
		status:      StatusActive,
		createdAt:   now, updatedAt: now,
		version: 1,
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func ReconstituteWithVersion(id, companyID uuid.UUID, code, name, description string, status Status, createdAt, updatedAt time.Time, deletedAt *time.Time, version uint64) (*Category, error) {
	c := &Category{
		id: id, companyID: companyID,
		code:        strings.ToUpper(strings.TrimSpace(code)),
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
		status:      status,
		createdAt:   createdAt, updatedAt: updatedAt,
		deletedAt: deletedAt,
		version:   version,
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Category) ID() uuid.UUID        { return c.id }
func (c *Category) CompanyID() uuid.UUID { return c.companyID }
func (c *Category) Code() string         { return c.code }
func (c *Category) Name() string         { return c.name }
func (c *Category) Description() string  { return c.description }
func (c *Category) Status() Status       { return c.status }
func (c *Category) Version() uint64      { return c.version }
func (c *Category) IsArchived() bool     { return c.deletedAt != nil }
func (c *Category) CreatedAt() time.Time { return c.createdAt }
func (c *Category) UpdatedAt() time.Time { return c.updatedAt }
func (c *Category) DeletedAt() *time.Time {
	if c.deletedAt == nil {
		return nil
	}
	at := *c.deletedAt
	return &at
}

func (c *Category) UpdateDetails(name, description string, now time.Time) error {
	if c.IsArchived() {
		return apperror.Conflict("cannot update archived category")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return apperror.Validation("category name is required")
	}
	c.name = name
	c.description = strings.TrimSpace(description)
	c.touch(now)
	return nil
}

func (c *Category) Activate(now time.Time) {
	if !c.IsArchived() {
		c.status = StatusActive
		c.touch(now)
	}
}
func (c *Category) Deactivate(now time.Time) {
	if !c.IsArchived() {
		c.status = StatusInactive
		c.touch(now)
	}
}

func (c *Category) Archive(now time.Time) error {
	if err := c.CanArchive(); err != nil {
		return err
	}
	c.deletedAt = &now
	c.touch(now)
	return nil
}

func (c *Category) CanArchive() error {
	if c.IsArchived() {
		return apperror.Conflict("already archived")
	}
	return nil
}

func (c *Category) validate() error {
	if c.id == uuid.Nil || c.companyID == uuid.Nil || c.code == "" || c.name == "" || !c.status.Valid() {
		return apperror.Validation("invalid category fields")
	}
	return nil
}

func (c *Category) touch(now time.Time) { c.updatedAt = now }
