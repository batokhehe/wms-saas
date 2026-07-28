// Package entity holds the auth module's domain types.
//
// LAYER RULE: entity imports nothing from this project except
// internal/shared/entity, and nothing from any web framework.
package entity

import (
	"strings"
	"time"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// Status is the lifecycle state of an account.
type Status string

const (
	// StatusActive can authenticate.
	StatusActive Status = "ACTIVE"
	// StatusInactive is deactivated — an offboarded employee, an unfinished
	// invitation. Reversible by an administrator.
	StatusInactive Status = "INACTIVE"
	// StatusLocked is a security hold — too many failed logins, a suspected
	// compromise. Semantically distinct from INACTIVE because the remedy is
	// different: unlocking is a security decision, reactivating is an
	// administrative one.
	StatusLocked Status = "LOCKED"
)

// Valid reports whether s is a known status. It backs the CHECK constraint on
// the users table, so an invalid value is rejected before it reaches SQL.
func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusInactive, StatusLocked:
		return true
	default:
		return false
	}
}

// User is an identity. It is deliberately NOT tenant-scoped: there is no
// CompanyID here and there will not be one.
//
// A person can belong to several companies — a 3PL operator works for multiple
// clients, a manager oversees two subsidiaries — so binding identity to a
// company would force one account per company, with duplicate credentials and
// no single account to lock when that person leaves. Authentication also has to
// work before any company context exists.
//
// Sprint 2 introduces a memberships table joining users to companies. Nothing
// in this type changes for that to happen.
type User struct {
	sharedentity.BaseEntity

	Email string `gorm:"type:citext;not null"`

	// PasswordHash is bcrypt output. It has no json tag anywhere in the API:
	// entities are never serialised to clients, and dto.UserResponse has no
	// corresponding field. See mapper/.
	PasswordHash string `gorm:"type:varchar(60);not null"`

	FullName string `gorm:"type:varchar(255);not null"`
	Status   Status `gorm:"type:varchar(16);not null;default:ACTIVE"`

	LastLoginAt     *time.Time
	EmailVerifiedAt *time.Time
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (User) TableName() string { return "users" }

// CanAuthenticate reports whether this account may log in.
//
// Only ACTIVE accounts can. Expressing it as a single method rather than
// scattering `u.Status == StatusActive` through the service means there is one
// place to change when email verification becomes mandatory — and one place to
// review when asking "who can get in?".
func (u *User) CanAuthenticate() bool {
	return u.Status == StatusActive
}

// IsLocked reports whether the account is under a security hold.
func (u *User) IsLocked() bool { return u.Status == StatusLocked }

// IsEmailVerified reports whether the address has been confirmed.
//
// Nothing enforces this yet: no verification flow exists in this sprint. The
// field and predicate are here so that turning verification on later is a
// change to CanAuthenticate, not a schema migration on a populated table.
func (u *User) IsEmailVerified() bool { return u.EmailVerifiedAt != nil }

// RecordLogin stamps the successful-authentication time.
//
// The clock is passed in rather than read from time.Now(), so a test can pin it.
func (u *User) RecordLogin(now time.Time) { u.LastLoginAt = &now }

// NormalizeEmail lower-cases and trims an address.
//
// The column is CITEXT, so comparison is already case-insensitive; normalising
// on the way in keeps the *stored* form canonical too, which matters for logs,
// exports and any future join that is not itself CITEXT.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// UserID is the identity primary key type. Naming it makes signatures
// self-documenting and stops a uuid.UUID for some other entity being passed
// where a user was meant.
type UserID = uuid.UUID
