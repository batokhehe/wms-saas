package entity

// Permission codes guarding the inventory endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package, and that rule has no exception for "just some string constants". So
// the inventory module declares its own. "What capabilities does inventory
// have?" is inventory vocabulary; RBAC owns the CATALOGUE — how codes are stored,
// granted and evaluated.
//
// The coupling is real and guarded rather than hidden: three places must agree,
// and permission_test.go asserts it.
//
//  1. these constants
//  2. rbac/entity.PermissionCatalogue()
//  3. migration 20260730100000_seed_inventory_permissions
//
// permission_test.go imports rbac/entity from a TEST-only package to compare 1
// against 2, so a drift fails the suite rather than becoming a permission that
// can never be granted.
const (
	// PermissionRead guards viewing stock positions.
	PermissionRead = "inventory.read"

	// PermissionCreate guards opening a new stock position.
	PermissionCreate = "inventory.create"

	// PermissionUpdate guards routine quantity movements — increase and decrease.
	PermissionUpdate = "inventory.update"

	// PermissionAdjust guards a manual absolute correction of on-hand.
	//
	// Separate from cycle counting and from routine movement: an adjustment
	// overrides the recorded count with no physical count behind it, so it is the
	// governance-sensitive, audit-heavy action — the inventory analogue of
	// warehouse.delete, and withheld from ADMIN for the same reason.
	PermissionAdjust = "inventory.adjust"

	// PermissionReserve guards promising and releasing available stock.
	PermissionReserve = "inventory.reserve"

	// PermissionTransfer guards moving stock in or out for a transfer.
	PermissionTransfer = "inventory.transfer"

	// PermissionLock guards freezing and unfreezing a position — a governance
	// hold that blocks every movement.
	PermissionLock = "inventory.lock"

	// PermissionCycleCount guards reconciling on-hand to a physical count.
	PermissionCycleCount = "inventory.cyclecount"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionAdjust,
		PermissionReserve,
		PermissionTransfer,
		PermissionLock,
		PermissionCycleCount,
	}
}
