package entity

import (
	"strings"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// System role names.
//
// These match the values the tenancy module stores in memberships.role. That
// correspondence is the join between the two modules, and it is why these are
// plain strings rather than a type imported from tenancy — importing it would
// break the no-cross-module-imports rule for a three-element enum.
//
// A compile-time guard is impossible here, so RBAC.md documents the coupling
// and the provisioner's tests assert the exact names.
const (
	SystemRoleOwner = "OWNER"
	SystemRoleAdmin = "ADMIN"
	SystemRoleStaff = "STAFF"
)

// SystemRoleNames lists the roles provisioned automatically for every company.
func SystemRoleNames() []string {
	return []string{SystemRoleOwner, SystemRoleAdmin, SystemRoleStaff}
}

// IsSystemRoleName reports whether name is one of the built-in roles.
func IsSystemRoleName(name string) bool {
	switch NormalizeRoleName(name) {
	case SystemRoleOwner, SystemRoleAdmin, SystemRoleStaff:
		return true
	default:
		return false
	}
}

// NormalizeRoleName canonicalises a role name.
//
// The column is CITEXT so comparison is already case-insensitive; normalising
// on the way in keeps the STORED form canonical too, which matters because this
// value is compared against memberships.role — a plain varchar that is NOT
// case-insensitive.
func NormalizeRoleName(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}

// Role is a named bundle of permissions within one company.
//
// It is tenant-owned: two companies may grant completely different permissions
// to the role they both call "ADMIN", and neither can see the other's
// definition.
type Role struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`

	// Name is the join key against memberships.role.
	Name string `gorm:"type:citext;not null"`

	Description string `gorm:"type:varchar(255);not null;default:''"`

	// IsSystem marks OWNER/ADMIN/STAFF. See CanDelete and CanRename.
	IsSystem bool `gorm:"not null;default:false"`
}

// TableName pins the table name.
func (Role) TableName() string { return "roles" }

// BelongsTo reports whether the role is owned by the given tenant. It backs the
// defence-in-depth check in the service layer.
func (r *Role) BelongsTo(companyID uuid.UUID) bool { return r.CompanyID == companyID }

// CanDelete reports whether the role may be removed.
//
// System roles may not. memberships.role points at them BY NAME with no foreign
// key, so deleting one would strand every member holding it: their role would
// resolve to nothing, they would silently lose all permissions, and there would
// be no way to re-grant without recreating the role by hand.
func (r *Role) CanDelete() bool { return !r.IsSystem }

// CanRename reports whether the role's name may change.
//
// Nothing may be renamed, system or custom — the same dangling-reference
// problem applies to any role a membership names. The API therefore exposes no
// rename at all; this method exists so the rule is stated in the domain rather
// than only implied by the absence of a field in the update DTO.
func (r *Role) CanRename() bool { return false }

// CanEditPermissions reports whether the role's grants may change.
//
// True even for system roles. A company legitimately wants to decide what its
// own ADMIN can do — it is the role's EXISTENCE and IDENTITY that are
// protected, not its contents. The one exception is OWNER, guarded separately
// in the service so that a company cannot lock itself out of its own account.
func (r *Role) CanEditPermissions() bool { return true }

// IsOwnerRole reports whether this is the company's OWNER role.
func (r *Role) IsOwnerRole() bool {
	return r.IsSystem && NormalizeRoleName(r.Name) == SystemRoleOwner
}

// RoleID identifies one role.
type RoleID = uuid.UUID

// RolePermission grants one permission to one role.
//
// It carries no CompanyID: the tenant is implied by RoleID, and duplicating it
// would create a second source of truth that could disagree with the role's
// own company — a denormalisation whose only possible outcome is a grant
// pointing at the wrong tenant.
type RolePermission struct {
	sharedentity.BaseEntity

	RoleID       uuid.UUID `gorm:"type:uuid;not null;index"`
	PermissionID uuid.UUID `gorm:"type:uuid;not null;index"`
}

// TableName pins the table name.
func (RolePermission) TableName() string { return "role_permissions" }

// DefaultPermissions returns the permissions a system role is provisioned with.
//
// These are SEED values, not runtime policy. Once a company exists its rows are
// the single source of truth, and an administrator may edit them freely — so
// this function is consulted exactly once per company, at provisioning.
//
// A non-system role name returns nil rather than an error: custom roles start
// empty and are populated explicitly, which is safer than inheriting a default
// nobody chose.
func DefaultPermissions(roleName string) []Code {
	switch NormalizeRoleName(roleName) {
	case SystemRoleOwner:
		// Everything. The owner must always be able to reach every part of
		// their own company, including granting themselves back a permission an
		// administrator removed — which is why the service also refuses to strip
		// the OWNER role.
		return PermissionCatalogue()

	case SystemRoleAdmin:
		// Operational: runs the company day to day, manages people, but cannot
		// destroy the tenant or restructure authorisation.
		//
		// company.delete is withheld because deleting the company is an
		// ownership decision, not an operational one. role.create/update/delete
		// and role.assign_permissions are withheld because an admin who can
		// edit roles can grant themselves anything — which would make the
		// distinction between ADMIN and OWNER cosmetic.
		return []Code{
			CompanyRead, CompanyUpdate,
			MembershipRead, MembershipInvite, MembershipRemove,
			RoleRead, PermissionRead,
			// Warehouses are operational infrastructure an admin runs.
			// warehouse.delete is withheld for the same reason as
			// company.delete: archiving a site is an ownership decision.
			WarehouseRead, WarehouseCreate, WarehouseUpdate, WarehouseActivate,
			// Locations are the rack layout an admin maintains. Unlike
			// warehouse.delete, LocationLock IS granted: locking a damaged bin
			// is a floor decision, and withholding it would leave the bin
			// pickable until an owner is available.
			LocationRead, LocationCreate, LocationUpdate, LocationLock,
			// Products are the catalogue an admin curates. product.discontinue
			// is withheld for the same reason as warehouse.delete: retiring an
			// article permanently (DISCONTINUED is terminal) is an ownership
			// decision, not an operational one.
			ProductRead, ProductCreate, ProductUpdate, ProductActivate,
			// Inventory is the stock an admin runs day to day, including locks
			// and cycle counts. inventory.adjust is withheld: a manual override
			// of the recorded count with no physical count behind it is a
			// governance decision, like warehouse.delete.
			InventoryRead, InventoryCreate, InventoryUpdate, InventoryReserve,
			InventoryTransfer, InventoryLock, InventoryCycleCount,
			// Suppliers are master data an admin curates end to end.
			SupplierRead, SupplierCreate, SupplierUpdate, SupplierActivate,
			// Customers are master data an admin curates end to end.
			CustomerRead, CustomerCreate, CustomerUpdate, CustomerActivate,
		}

	case SystemRoleStaff:
		// Read-only over the administrative surface. Staff perform warehouse
		// operations, and those permissions will be added to this list by the
		// sprint that implements them — not speculatively now.
		return []Code{
			CompanyRead, MembershipRead, RoleRead, PermissionRead,
			// Staff operate WITHIN a warehouse and its locations; they do not
			// define either. Permissions for acting on STOCK belong to the
			// Inventory sprint.
			WarehouseRead, LocationRead,
			// Staff pick and receive the products the catalogue defines, but
			// defining the catalogue is not their decision.
			ProductRead,
			// Staff DO act on stock — this is the sprint that grants it. They
			// move quantity, reserve, transfer and count, but do not open new
			// positions, make manual adjustments or place governance locks.
			InventoryRead, InventoryUpdate, InventoryReserve,
			InventoryTransfer, InventoryCycleCount,
			// Staff read the supplier catalogue but do not curate it.
			SupplierRead,
			// Staff read the customer catalogue but do not curate it.
			CustomerRead,
		}

	default:
		return nil
	}
}

// DefaultDescription returns the seeded description for a system role.
func DefaultDescription(roleName string) string {
	switch NormalizeRoleName(roleName) {
	case SystemRoleOwner:
		return "Full access to the company, including deletion and role management."
	case SystemRoleAdmin:
		return "Manages members and company settings."
	case SystemRoleStaff:
		return "Day-to-day operations with read access to administration."
	default:
		return ""
	}
}
