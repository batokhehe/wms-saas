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

type Brand struct {
	id, companyID           uuid.UUID
	code, name, description string
	status                  Status
	createdAt, updatedAt    time.Time
	deletedAt               *time.Time
	version                 uint64
}

func New(id, companyID uuid.UUID, code, name, description string, now time.Time) (*Brand, error) {
	b := &Brand{
		id: id, companyID: companyID,
		code:        strings.ToUpper(strings.TrimSpace(code)),
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
		status:      StatusActive,
		createdAt:   now, updatedAt: now,
		version: 1,
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b, nil
}

func ReconstituteWithVersion(id, companyID uuid.UUID, code, name, description string, status Status, createdAt, updatedAt time.Time, deletedAt *time.Time, version uint64) (*Brand, error) {
	b := &Brand{
		id: id, companyID: companyID,
		code:        strings.ToUpper(strings.TrimSpace(code)),
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
		status:      status,
		createdAt:   createdAt, updatedAt: updatedAt,
		deletedAt: deletedAt,
		version:   version,
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Brand) ID() uuid.UUID        { return b.id }
func (b *Brand) CompanyID() uuid.UUID { return b.companyID }
func (b *Brand) Code() string         { return b.code }
func (b *Brand) Name() string         { return b.name }
func (b *Brand) Description() string  { return b.description }
func (b *Brand) Status() Status       { return b.status }
func (b *Brand) Version() uint64      { return b.version }
func (b *Brand) IsArchived() bool     { return b.deletedAt != nil }
func (b *Brand) CreatedAt() time.Time { return b.createdAt }
func (b *Brand) UpdatedAt() time.Time { return b.updatedAt }
func (b *Brand) DeletedAt() *time.Time {
	if b.deletedAt == nil {
		return nil
	}
	at := *b.deletedAt
	return &at
}

func (b *Brand) UpdateDetails(name, description string, now time.Time) error {
	if b.IsArchived() {
		return apperror.Conflict("cannot update archived brand")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return apperror.Validation("brand name is required")
	}
	b.name = name
	b.description = strings.TrimSpace(description)
	b.touch(now)
	return nil
}

func (b *Brand) Activate(now time.Time) {
	if !b.IsArchived() {
		b.status = StatusActive
		b.touch(now)
	}
}
func (b *Brand) Deactivate(now time.Time) {
	if !b.IsArchived() {
		b.status = StatusInactive
		b.touch(now)
	}
}

func (b *Brand) Archive(now time.Time) error {
	if err := b.CanArchive(); err != nil {
		return err
	}
	b.deletedAt = &now
	b.touch(now)
	return nil
}

func (b *Brand) CanArchive() error {
	if b.IsArchived() {
		return apperror.Conflict("already archived")
	}
	return nil
}

func (b *Brand) validate() error {
	if b.id == uuid.Nil || b.companyID == uuid.Nil || b.code == "" || b.name == "" || !b.status.Valid() {
		return apperror.Validation("invalid brand fields")
	}
	return nil
}

func (b *Brand) touch(now time.Time) { b.updatedAt = now }
