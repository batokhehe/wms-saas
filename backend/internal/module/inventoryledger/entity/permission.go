package entity

// Permission codes guarding the inventory-ledger endpoints.
//
// The ledger is READ-ONLY over HTTP: entries are written by the Inventory module
// through the service, never by a client. So there is exactly one code — there is
// no create, update or delete capability to grant, and adding one would imply an
// operation the module deliberately does not offer.
//
// ModuleConvention §6 forbids importing another module's entity package, so this
// module declares its own code; RBAC owns the catalogue. permission_test.go
// guards the two against drift from a test-only package.
const (
	// PermissionRead guards viewing ledger entries.
	PermissionRead = "inventoryledger.read"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{PermissionRead}
}
