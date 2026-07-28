package entity

// Permission codes guarding the customer endpoints.
//
// ModuleConvention §6 forbids importing another module's entity package, so the
// customer module declares its own codes; RBAC owns the catalogue. The coupling
// is guarded by permission_test.go from a test-only package.
const (
	// PermissionRead guards viewing customers.
	PermissionRead = "customer.read"

	// PermissionCreate guards registering a new customer.
	PermissionCreate = "customer.create"

	// PermissionUpdate guards editing a customer's attributes.
	PermissionUpdate = "customer.update"

	// PermissionActivate guards activating and deactivating a customer.
	//
	// Separate from PermissionUpdate: editing a phone number is routine, while
	// deactivating a customer stops every new sales order to them.
	PermissionActivate = "customer.activate"
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
