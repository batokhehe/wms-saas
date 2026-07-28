package entity

// Permission codes guarding the location endpoints.
//
// Declared here rather than imported from RBAC, for the reason set out in the
// warehouse module: ModuleConvention §6 forbids a module importing another's
// entity package, and that rule has no exception for "just some string
// constants" — once one module does it, the next one does too.
//
// RBAC owns the CATALOGUE (how codes are stored, granted and evaluated); each
// domain owns the names of its own capabilities.
//
// Three places must agree, and a test asserts it:
//
//  1. these constants
//  2. rbac/entity.PermissionCatalogue()
//  3. migration 20260726100001_seed_location_permissions
//
// permission_test.go imports rbac/entity from a _test package — a test-only
// dependency that does not appear in the production build graph — so the
// architectural rule holds where it matters while the drift is still caught.
const (
	// PermissionRead guards viewing locations.
	PermissionRead = "location.read"

	// PermissionCreate guards defining a new location.
	PermissionCreate = "location.create"

	// PermissionUpdate guards editing attributes: barcode, capacity, picking
	// priority, mixed-SKU and overflow policy, and the ACTIVE/INACTIVE/
	// MAINTENANCE transitions.
	PermissionUpdate = "location.update"

	// PermissionLock guards locking and unlocking, and archiving.
	//
	// Separate from PermissionUpdate deliberately: relabelling a bin is
	// routine, while locking one takes it out of service and blocks every
	// putaway and pick that would have used it. Archiving is grouped here
	// rather than under update for the same reason — it removes a place from
	// the layout.
	PermissionLock = "location.lock"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionLock,
	}
}
