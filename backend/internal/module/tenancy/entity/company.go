// Package entity holds the tenancy module's domain types.
//
// LAYER RULE: entity imports nothing from this project except
// internal/shared/entity, and nothing from any web framework.
package entity

import (
	"strings"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// CompanyStatus is the lifecycle state of a tenant.
type CompanyStatus string

const (
	// CompanyActive can be used normally.
	CompanyActive CompanyStatus = "ACTIVE"

	// CompanyInactive is dormant — an ended contract, a trial that lapsed.
	// Reversible by an administrator.
	CompanyInactive CompanyStatus = "INACTIVE"

	// CompanySuspended is an enforcement hold — non-payment, a terms violation.
	//
	// Semantically distinct from INACTIVE because the remedy differs: lifting a
	// suspension is a commercial decision, reactivating is an administrative
	// one. Collapsing them into a single `disabled` flag would lose the reason,
	// and the reason is what a support agent needs first.
	CompanySuspended CompanyStatus = "SUSPENDED"
)

// Valid reports whether s is a known status. It mirrors the CHECK constraint on
// the companies table, so an invalid value is rejected before it reaches SQL.
func (s CompanyStatus) Valid() bool {
	switch s {
	case CompanyActive, CompanyInactive, CompanySuspended:
		return true
	default:
		return false
	}
}

// Company is a tenant.
//
// It carries no owner_id and no user_id. The link between a company and the
// people in it lives entirely in Membership — putting an owner here would
// create a second source of truth for "who runs this company" that could
// disagree with the OWNER membership row.
//
// It also carries no CompanyID of its own: a company IS the tenant, so it is
// the root of the tenancy graph rather than a member of it. This is the one
// persisted entity in the system that is legitimately not tenant-scoped.
type Company struct {
	sharedentity.BaseEntity

	// Code is the human-facing tenant identifier. CITEXT, so "acme" and "ACME"
	// are the same company.
	Code string `gorm:"type:citext;not null"`

	Name string `gorm:"type:varchar(255);not null"`

	// Contact details are optional: a company is created during onboarding,
	// before the operator has necessarily supplied them.
	Email string `gorm:"type:citext"`
	Phone string `gorm:"type:varchar(32)"`

	// Logo is an object-store KEY, never a URL. The bucket and CDN host are
	// deployment concerns that change independently of the data.
	Logo string `gorm:"type:varchar(512)"`

	Address string `gorm:"type:text"`

	Status CompanyStatus `gorm:"type:varchar(16);not null;default:ACTIVE"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (Company) TableName() string { return "companies" }

// IsOperational reports whether the company may be used for business
// operations.
//
// Only ACTIVE companies can. Expressing it as one method rather than scattering
// `c.Status == CompanyActive` through the services means there is a single
// place to change when, say, a grace period is added for SUSPENDED tenants —
// and a single place to review when asking "who can transact?".
func (c *Company) IsOperational() bool { return c.Status == CompanyActive }

// IsSuspended reports whether the company is under an enforcement hold.
func (c *Company) IsSuspended() bool { return c.Status == CompanySuspended }

// NormalizeCode canonicalises a company code.
//
// The column is CITEXT so comparison is already case-insensitive; normalising
// on the way in keeps the STORED form canonical too, which matters for exports,
// document headers and any future join that is not itself CITEXT.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// CompanyID is the tenant identifier type. Naming it makes tenant-scoped
// signatures self-documenting and stops a uuid.UUID for some other entity being
// passed where a company was meant.
type CompanyID = uuid.UUID
