package entity

// Permission codes guarding the product endpoints.
//
// # Why these are declared here and not imported from RBAC
//
// ModuleConvention §6 forbids a module from importing another module's entity
// package, and that rule has no exception for "just some string constants" —
// once one module imports another's entity, the next one does too, and the
// dependency graph stops being reviewable.
//
// So the product module declares its own. "What capabilities does a product
// have?" is product vocabulary. RBAC owns the CATALOGUE — how codes are stored,
// granted and evaluated — while each domain owns the names of its own
// capabilities.
//
// The coupling is real and is guarded rather than hidden: three places must
// agree, and a test asserts it.
//
//  1. these constants
//  2. rbac/entity.PermissionCatalogue()
//  3. migration 20260728100001_seed_product_permissions
//
// permission_test.go imports rbac/entity to compare 1 against 2. That is a
// TEST-only dependency — it does not appear in the production build graph, so
// the architectural rule holds where it matters — and it means a drift fails the
// suite rather than becoming a permission that can never be granted.
const (
	// PermissionRead guards viewing products.
	PermissionRead = "product.read"

	// PermissionCreate guards registering a new product.
	PermissionCreate = "product.create"

	// PermissionUpdate guards editing a product: name, description, category,
	// brand, measurements, shelf life, tracking, barcodes and alternate units.
	// These are the routine catalogue edits, grouped under one capability.
	PermissionUpdate = "product.update"

	// PermissionActivate guards commissioning and decommissioning — moving a
	// product between DRAFT and ACTIVE.
	//
	// Separate from PermissionUpdate deliberately: editing an attribute is
	// routine, while declaring a product ready for operations is a catalogue
	// decision. Granting them together would mean anyone who can fix a typo can
	// also put a half-configured product into operation.
	PermissionActivate = "product.activate"

	// PermissionDiscontinue guards permanent retirement. DISCONTINUED is
	// terminal, so this is the destructive capability — the product-module
	// analogue of warehouse.delete, and withheld from ADMIN for the same reason:
	// retiring an article from the catalogue is an ownership decision, not an
	// operational one.
	PermissionDiscontinue = "product.discontinue"
)

// Permissions lists every code this module enforces, for the drift guard.
func Permissions() []string {
	return []string{
		PermissionRead,
		PermissionCreate,
		PermissionUpdate,
		PermissionActivate,
		PermissionDiscontinue,
	}
}
