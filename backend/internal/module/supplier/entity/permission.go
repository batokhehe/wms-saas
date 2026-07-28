package entity

// Permission codes guarding the supplier endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package. So the supplier module declares its own codes; RBAC owns the
// CATALOGUE. The coupling is guarded by permission_test.go, which compares these
// against rbac/entity.PermissionCatalogue() from a test-only package.
const (
	// PermissionRead guards viewing suppliers.
	PermissionRead = "supplier.read"

	// PermissionCreate guards registering a new supplier.
	PermissionCreate = "supplier.create"

	// PermissionUpdate guards editing a supplier's attributes.
	PermissionUpdate = "supplier.update"

	// PermissionActivate guards activating and deactivating a supplier.
	//
	// Separate from PermissionUpdate: editing a phone number is routine, while
	// deactivating a supplier stops every new purchase order to them, which is a
	// governance decision.
	PermissionActivate = "supplier.activate"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionActivate,
	}
}
