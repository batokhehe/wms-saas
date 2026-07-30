package entity

// Permission codes guarding the stock-transfer endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package. So this module declares its own codes; RBAC owns the CATALOGUE. The
// coupling is guarded by permission_test.go, which compares these against
// rbac/entity.PermissionCatalogue() from a test-only package.
//
// # Why each lifecycle step has its own code
//
// The three lifecycle codes are separate because they authorise materially
// different things. Confirming approves a movement on paper. Completing MOVES
// REAL STOCK — it is the only one of the three that changes a balance, so it is
// the one a company is most likely to restrict to a supervisor. Cancelling
// voids an approved document. Collapsing them into stocktransfer.update would
// mean anyone who can fix a typo in the remarks can also execute the movement.
const (
	// PermissionRead guards viewing transfers.
	PermissionRead = "stocktransfer.read"

	// PermissionCreate guards drafting a new transfer.
	PermissionCreate = "stocktransfer.create"

	// PermissionUpdate guards editing a DRAFT transfer's header and lines.
	PermissionUpdate = "stocktransfer.update"

	// PermissionConfirm guards approving a draft for execution.
	PermissionConfirm = "stocktransfer.confirm"

	// PermissionComplete guards executing the movement. This is the code that
	// authorises an actual change to stock.
	PermissionComplete = "stocktransfer.complete"

	// PermissionCancel guards abandoning a transfer before execution.
	PermissionCancel = "stocktransfer.cancel"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionConfirm,
		PermissionComplete,
		PermissionCancel,
	}
}
