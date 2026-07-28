package entity

// Permission codes guarding the warehouse endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package, and that rule has no exception for "just some string constants" —
// once one module imports another's entity, the next one does too, and the
// dependency graph stops being reviewable.
//
// So the warehouse module declares its own. This is not arbitrary duplication:
// "what capabilities does a warehouse have?" is warehouse vocabulary. RBAC owns
// the CATALOGUE — how codes are stored, granted and evaluated — while each
// domain owns the names of its own capabilities.
//
// The coupling is real and is guarded rather than hidden: three places must
// agree, and a test asserts it.
//
//  1. these constants
//  2. rbac/entity.PermissionCatalogue()
//  3. migration 20260725100001_seed_warehouse_permissions
//
// entity_permission_test.go imports rbac/entity to compare 1 against 2. That is
// a TEST-only dependency — it does not appear in the production build graph, so
// the architectural rule holds where it matters — and it means a drift fails the
// suite rather than becoming a permission that can never be granted.
const (
	// PermissionRead guards viewing warehouses.
	PermissionRead = "warehouse.read"

	// PermissionCreate guards registering a new warehouse.
	PermissionCreate = "warehouse.create"

	// PermissionUpdate guards editing attributes: name, description, address,
	// contact, zone assignments.
	PermissionUpdate = "warehouse.update"

	// PermissionDelete guards ARCHIVING and SUSPENDING.
	//
	// Both are governance actions that take a site out of service, and neither
	// erases anything — a warehouse is never hard-deleted. The code is named
	// "delete" because that is the REST verb clients expect, not because
	// anything is destroyed.
	PermissionDelete = "warehouse.delete"

	// PermissionActivate guards commissioning and decommissioning.
	//
	// Separate from PermissionUpdate deliberately: editing an address is
	// routine, while declaring a site fit to receive and ship stock is a
	// commissioning decision. Granting them together would mean anyone who can
	// fix a typo can also put a half-configured warehouse into operation.
	PermissionActivate = "warehouse.activate"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionDelete,
		PermissionActivate,
	}
}
