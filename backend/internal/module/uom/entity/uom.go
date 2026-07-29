package entity

import (
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"strings"
	"time"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

func (s Status) Valid() bool    { return s == StatusActive || s == StatusInactive }
func (s Status) String() string { return string(s) }

type UOM struct {
	id                      uuid.UUID
	code, name, description string
	status                  Status
	createdAt, updatedAt    time.Time
	version                 uint64
}

func New(id uuid.UUID, code, name, description string, now time.Time) (*UOM, error) {
	u := &UOM{id: id, code: strings.ToUpper(strings.TrimSpace(code)), name: strings.TrimSpace(name), description: strings.TrimSpace(description), status: StatusActive, createdAt: now, updatedAt: now, version: 1}
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u, nil
}
func Reconstitute(id uuid.UUID, code, name, description string, status Status, createdAt, updatedAt time.Time) (*UOM, error) {
	return ReconstituteWithVersion(id, code, name, description, status, createdAt, updatedAt, 1)
}
func ReconstituteWithVersion(id uuid.UUID, code, name, description string, status Status, createdAt, updatedAt time.Time, version uint64) (*UOM, error) {
	u := &UOM{id: id, code: strings.ToUpper(strings.TrimSpace(code)), name: strings.TrimSpace(name), description: strings.TrimSpace(description), status: status, createdAt: createdAt, updatedAt: updatedAt, version: version}
	if err := u.validate(); err != nil {
		return nil, err
	}
	return u, nil
}
func (u *UOM) ID() uuid.UUID        { return u.id }
func (u *UOM) Code() string         { return u.code }
func (u *UOM) Name() string         { return u.name }
func (u *UOM) Description() string  { return u.description }
func (u *UOM) Status() Status       { return u.status }
func (u *UOM) CreatedAt() time.Time { return u.createdAt }
func (u *UOM) UpdatedAt() time.Time { return u.updatedAt }
func (u *UOM) Version() uint64      { return u.version }

// UpdateDetails modifies the mutable attributes of the UOM.
func (u *UOM) UpdateDetails(name string, description string, now time.Time) error {
	normalizedName := strings.TrimSpace(name)
	if normalizedName == "" {
		return apperror.Validation("uom name is required")
	}

	normalizedDesc := strings.TrimSpace(description)
	const maxDescLength = 2000
	if len(normalizedDesc) > maxDescLength {
		return apperror.Validation("uom description must be at most 2000 characters")
	}

	if normalizedName == u.name && normalizedDesc == u.description {
		return nil
	}

	u.name = normalizedName
	u.description = normalizedDesc
	u.touch(now)

	return nil
}

func (u *UOM) Activate(now time.Time)   { u.status = StatusActive; u.touch(now) }
func (u *UOM) Deactivate(now time.Time) { u.status = StatusInactive; u.touch(now) }
func (u *UOM) validate() error {
	if u.id == uuid.Nil || u.code == "" || u.name == "" || !u.status.Valid() {
		return apperror.Validation("uom id, code, name and valid status are required")
	}
	return nil
}

func (u *UOM) touch(now time.Time) {
	u.updatedAt = now
}
