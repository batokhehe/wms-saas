package entity

// Permission codes guarding the purchase-order endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package. So this module declares its own codes; RBAC owns the CATALOGUE. The
// coupling is guarded by permission_test.go, which compares these against
// rbac/entity.PermissionCatalogue() from a test-only package.
//
// # Why approve and cancel are separate from update
//
// Editing a draft is clerical. APPROVING commits the company to buy something
// and unlocks the whole inbound chain — it is the point at which the document
// becomes a commitment to a supplier. Cancelling withdraws that commitment.
// Folding either into purchaseorder.update would mean anyone who can fix a typo
// in the remarks can also commit the company's money.
const (
	// PermissionRead guards viewing purchase orders.
	PermissionRead = "purchaseorder.read"

	// PermissionCreate guards drafting a new purchase order.
	PermissionCreate = "purchaseorder.create"

	// PermissionUpdate guards editing a DRAFT order's header and lines.
	PermissionUpdate = "purchaseorder.update"

	// PermissionApprove guards committing the order.
	PermissionApprove = "purchaseorder.approve"

	// PermissionCancel guards withdrawing the order.
	PermissionCancel = "purchaseorder.cancel"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionApprove,
		PermissionCancel,
	}
}
