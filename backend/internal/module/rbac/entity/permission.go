// Package entity holds the RBAC module's domain types.
//
// LAYER RULE: entity imports nothing from this project except
// internal/shared/entity, and nothing from any web framework.
package entity

import (
	"strings"

	"github.com/google/uuid"

	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// Code is a permission's stable, machine-readable identifier.
//
// It is the API of the authorisation system: it appears in route declarations,
// in the middleware, and in every grant row. A released code may never be
// renamed — a rename silently revokes access everywhere it was granted, with no
// error anywhere to indicate it happened. Add a new code and deprecate the old
// one instead.
type Code string

// The permission catalogue.
//
// These constants mirror the rows seeded by migration 20260724100000. Both must
// change together; PermissionCatalogue() below is checked against the database
// at boot so a drift fails loudly rather than becoming a permission that can
// never be granted.
const (
	CompanyRead   Code = "company.read"
	CompanyUpdate Code = "company.update"
	CompanyDelete Code = "company.delete"

	MembershipRead   Code = "membership.read"
	MembershipInvite Code = "membership.invite"
	MembershipRemove Code = "membership.remove"

	RoleRead              Code = "role.read"
	RoleCreate            Code = "role.create"
	RoleUpdate            Code = "role.update"
	RoleDelete            Code = "role.delete"
	RoleAssignPermissions Code = "role.assign_permissions"

	PermissionRead Code = "permission.read"

	// Warehouse capabilities, added by migration 20260725100001.
	//
	// WarehouseActivate is separate from WarehouseUpdate deliberately: editing a
	// warehouse's address is routine, while activating one declares it fit to
	// receive and ship stock. Granting them together would mean anyone who can
	// fix a typo can also put a half-configured site into operation.
	//
	// WarehouseDelete guards ARCHIVING, not erasure — a warehouse is never hard
	// deleted. See docs/Warehouse.md §5.
	WarehouseRead     Code = "warehouse.read"
	WarehouseCreate   Code = "warehouse.create"
	WarehouseUpdate   Code = "warehouse.update"
	WarehouseDelete   Code = "warehouse.delete"
	WarehouseActivate Code = "warehouse.activate"

	// Storage location capabilities, added by migration 20260726100001.
	//
	// LocationLock is separate from LocationUpdate deliberately: relabelling a
	// bin is routine, while locking one takes it out of service and blocks
	// every putaway and pick that would have used it.
	LocationRead   Code = "location.read"
	LocationCreate Code = "location.create"
	LocationUpdate Code = "location.update"
	LocationLock   Code = "location.lock"

	// Product capabilities, added by migration 20260728100001.
	//
	// ProductActivate is separate from ProductUpdate deliberately: editing an
	// attribute is routine, while declaring a product ready for operations is a
	// catalogue decision.
	//
	// ProductDiscontinue guards permanent retirement — DISCONTINUED is terminal —
	// and is the destructive capability, the analogue of warehouse.delete. It is
	// withheld from ADMIN for the same reason: retiring an article is an
	// ownership decision. See docs/Product.md §9.
	ProductRead        Code = "product.read"
	ProductCreate      Code = "product.create"
	ProductUpdate      Code = "product.update"
	ProductActivate    Code = "product.activate"
	ProductDiscontinue Code = "product.discontinue"

	// Inventory capabilities, added by migration 20260730100000.
	//
	// InventoryAdjust is separate from InventoryUpdate and InventoryCycleCount
	// deliberately: an adjustment overrides the recorded count with no physical
	// count behind it, so it is the governance-sensitive action — withheld from
	// ADMIN like warehouse.delete. See docs/Inventory.md.
	InventoryRead       Code = "inventory.read"
	InventoryCreate     Code = "inventory.create"
	InventoryUpdate     Code = "inventory.update"
	InventoryAdjust     Code = "inventory.adjust"
	InventoryReserve    Code = "inventory.reserve"
	InventoryTransfer   Code = "inventory.transfer"
	InventoryLock       Code = "inventory.lock"
	InventoryCycleCount Code = "inventory.cyclecount"

	// Supplier capabilities, added by migration 20260731100001.
	//
	// SupplierActivate is separate from SupplierUpdate: editing a phone number is
	// routine, while deactivating a supplier stops every new purchase order to
	// them.
	SupplierRead     Code = "supplier.read"
	SupplierCreate   Code = "supplier.create"
	SupplierUpdate   Code = "supplier.update"
	SupplierActivate Code = "supplier.activate"

	// Customer capabilities, added by migration 20260801100001. The structural
	// sibling of the supplier codes.
	// Inventory-ledger capability, added by migration 20260808100001. There is
	// exactly ONE: the ledger is append-only and read-only, so there is no write
	// capability to grant.
	InventoryLedgerRead Code = "inventoryledger.read"

	// Catalogue master data, added by migration 20260809100000. These codes were
	// enforced by the brand, category and product routes but were absent from the
	// catalogue, so they could never be granted and those endpoints answered 403
	// to every caller including OWNER.
	CategoryRead   Code = "category.read"
	CategoryCreate Code = "category.create"
	CategoryUpdate Code = "category.update"
	CategoryDelete Code = "category.delete"

	BrandRead   Code = "brand.read"
	BrandCreate Code = "brand.create"
	BrandUpdate Code = "brand.update"
	BrandDelete Code = "brand.delete"

	// ProductDelete guards archiving a product; the product routes required it
	// while the catalogue listed only product.activate/discontinue.
	ProductDelete Code = "product.delete"

	CustomerRead     Code = "customer.read"
	CustomerCreate   Code = "customer.create"
	CustomerUpdate   Code = "customer.update"
	CustomerActivate Code = "customer.activate"
)

// String renders the code.
func (c Code) String() string { return string(c) }

// NormalizeCode canonicalises a permission code.
func NormalizeCode(code string) Code {
	return Code(strings.ToLower(strings.TrimSpace(code)))
}

// PermissionCatalogue is every code the software can enforce.
//
// Only capabilities that EXIST are listed. A permission for a module that
// cannot be exercised is a promise the software does not keep, and it would
// appear in every role editor as an option that does nothing.
func PermissionCatalogue() []Code {
	return []Code{
		CompanyRead, CompanyUpdate, CompanyDelete,
		MembershipRead, MembershipInvite, MembershipRemove,
		RoleRead, RoleCreate, RoleUpdate, RoleDelete, RoleAssignPermissions,
		PermissionRead,
		WarehouseRead, WarehouseCreate, WarehouseUpdate,
		WarehouseDelete, WarehouseActivate,
		LocationRead, LocationCreate, LocationUpdate, LocationLock,
		ProductRead, ProductCreate, ProductUpdate,
		ProductActivate, ProductDiscontinue,
		InventoryRead, InventoryCreate, InventoryUpdate, InventoryAdjust,
		InventoryReserve, InventoryTransfer, InventoryLock, InventoryCycleCount,
		SupplierRead, SupplierCreate, SupplierUpdate, SupplierActivate,
		CustomerRead, CustomerCreate, CustomerUpdate, CustomerActivate,
		InventoryLedgerRead,
		CategoryRead, CategoryCreate, CategoryUpdate, CategoryDelete,
		BrandRead, BrandCreate, BrandUpdate, BrandDelete,
		ProductDelete,
	}
}

// Permission is one capability the software can enforce.
//
// It is deliberately NOT tenant-owned: there is no CompanyID field and the
// table has no company_id column. A permission names something the code can do,
// which is identical for every customer. What each company controls is the
// mapping from its roles to these permissions — see RolePermission.
type Permission struct {
	sharedentity.BaseEntity

	Code Code   `gorm:"type:varchar(64);not null"`
	Name string `gorm:"type:varchar(128);not null"`

	// Module groups permissions for display. Presentation metadata only —
	// never consulted when deciding whether an action is allowed.
	Module string `gorm:"type:varchar(64);not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (Permission) TableName() string { return "permissions" }

// PermissionID identifies one capability.
type PermissionID = uuid.UUID

// Set is a resolved collection of permission codes.
//
// A map rather than a slice because its only operation is membership testing,
// once per authorised request: O(1) beats a linear scan over a set that will
// grow with every feature module.
type Set map[Code]struct{}

// NewSet builds a Set from codes.
func NewSet(codes ...Code) Set {
	set := make(Set, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	return set
}

// Has reports whether the set grants code.
func (s Set) Has(code Code) bool {
	_, ok := s[code]
	return ok
}

// HasAll reports whether the set grants every code. An empty requirement is
// satisfied — a route that demands nothing is open to any authorised caller.
func (s Set) HasAll(codes ...Code) bool {
	for _, code := range codes {
		if !s.Has(code) {
			return false
		}
	}
	return true
}

// HasAny reports whether the set grants at least one code.
//
// Used where several permissions each independently justify an action. An
// empty requirement is NOT satisfied: "any of nothing" cannot be true, and
// returning true would silently open a route whose requirement list was
// accidentally left empty.
func (s Set) HasAny(codes ...Code) bool {
	for _, code := range codes {
		if s.Has(code) {
			return true
		}
	}
	return false
}

// Codes returns the set's contents as a slice, for serialisation.
//
// Order is not guaranteed; callers that display them should sort.
func (s Set) Codes() []Code {
	codes := make([]Code, 0, len(s))
	for code := range s {
		codes = append(codes, code)
	}
	return codes
}

// Len reports how many permissions the set grants.
func (s Set) Len() int { return len(s) }
