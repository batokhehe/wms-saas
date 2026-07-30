package entity

// Permission codes guarding the goods-receipt endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package. So this module declares its own codes; RBAC owns the CATALOGUE. The
// coupling is guarded by permission_test.go, which compares these against
// rbac/entity.PermissionCatalogue() from a test-only package.
//
// # Why receive is its own code
//
// PermissionReceive is the one that POSTS STOCK. Confirming says the delivery was
// checked; receiving books it into inventory and appends to the ledger, which is
// the only step here that changes a balance. Folding it into
// goodsreceipt.update would mean anyone who can fix a typo in the remarks can
// also create inventory out of nothing.
const (
	// PermissionRead guards viewing goods receipts.
	PermissionRead = "goodsreceipt.read"

	// PermissionCreate guards drafting a new goods receipt.
	PermissionCreate = "goodsreceipt.create"

	// PermissionUpdate guards editing a DRAFT receipt's header and lines.
	PermissionUpdate = "goodsreceipt.update"

	// PermissionConfirm guards checking a draft off for posting.
	PermissionConfirm = "goodsreceipt.confirm"

	// PermissionReceive guards posting the stock into inventory.
	PermissionReceive = "goodsreceipt.receive"

	// PermissionCancel guards abandoning a receipt before posting.
	PermissionCancel = "goodsreceipt.cancel"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionConfirm,
		PermissionReceive,
		PermissionCancel,
	}
}
