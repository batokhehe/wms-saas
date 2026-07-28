// Package entity holds the module's domain types.
//
// LAYER RULE: entity imports nothing from this project except
// internal/shared/entity, and nothing from any web framework. It is the
// innermost layer — everything may depend on it, it depends on nothing.
//
// GORM struct tags are tolerated here (inert metadata, not behaviour), as is
// the gorm.DeletedAt carried by the embedded BaseEntity. See
// docs/EntityConvention.md for why that exception is drawn where it is.
package entity

import (
	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// TenantID is the company identifier type. Naming it makes tenant-scoped
// signatures self-documenting and prevents a plain uuid.UUID for some other
// entity from being passed where a company was meant.
type TenantID = uuid.UUID

// Resource is the placeholder domain type. Rename it to the real aggregate
// (Product, Warehouse, StockMovement) when copying this template.
//
// Embedding BaseEntity is mandatory for every persisted entity. It supplies the
// identifier, the audit timestamps and — most importantly — the gorm.DeletedAt
// that makes soft deletion automatic on every query.
type Resource struct {
	sharedentity.BaseEntity

	// CompanyID is the tenant discriminator. EVERY tenant-owned entity must
	// carry it, and every query must filter on it. A missing tenant filter is
	// not a bug that returns too many rows — it is a cross-tenant data leak.
	CompanyID uuid.UUID `gorm:"type:uuid;not null;index:idx_resources_company" json:"-"`

	Name string `gorm:"type:varchar(255);not null" json:"name"`
}

// TableName pins the table name so a future struct rename cannot silently
// change the schema GORM targets.
func (Resource) TableName() string { return "resources" }

// BelongsTo reports whether the resource is owned by the given tenant. It backs
// the defence-in-depth check in the service layer.
//
// Domain behaviour belongs on the entity. A method like this is preferable to
// scattering `r.CompanyID == companyID` across services, where one inverted
// comparison becomes a bug nobody notices.
func (r *Resource) BelongsTo(companyID TenantID) bool { return r.CompanyID == companyID }
