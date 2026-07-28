// Package service orchestrates the Product aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.Product. The service loads an aggregate, gathers any
// cross-aggregate FACTS the rule needs, calls one domain method, persists and
// publishes.
//
// The test for whether a rule belongs here: can the aggregate answer it by
// looking only at itself? "Is DISCONTINUED terminal?" — yes, that is the
// aggregate's. "Is this SKU already taken?" — no, that is a question about a SET
// (a specification), which only the repository can answer. "Does this category
// exist?" — no, a category is another aggregate, so a VERIFIER answers it.
package service

import (
	"context"

	"github.com/google/uuid"
)

// This file declares FOUR extension points. None is implemented here, and none
// will be implemented by this module.
//
// They share the shape every prior sprint used: the product module declares the
// interface it needs, a future module implements it, and bootstrap injects the
// implementation. No file in this package changes when that happens — which is
// the property the service tests assert by substituting fakes.
//
// Every default is a NAMED type rather than nil. A nil verifier would make "no
// verifier configured" and "the verifier permits it" indistinguishable, so a
// wiring mistake would silently disable a referential check.

// ---------- CategoryVerifier ----------

// CategoryVerifier confirms a category exists and belongs to a company.
//
// entity.Product.AssignCategory validates that a category id is well-formed. It
// cannot validate that the category EXISTS, because categories belong to the
// future Category aggregate — and a product aggregate that loaded a category
// aggregate would collapse the boundary that keeps each independently
// consistent. So the service asks this verifier before asking the aggregate to
// assign it.
//
// Implemented by the Category sprint; until then products.category_id is an
// application-level reference with no foreign key, exactly as warehouse's zone
// ids were. See docs/Product.md §5.
type CategoryVerifier interface {
	// VerifyCategory returns nil when the category exists in the company.
	//
	// It takes a companyID as well as the category id so an implementation
	// cannot accidentally query across tenants — the discipline the repositories
	// follow.
	VerifyCategory(ctx context.Context, companyID, categoryID uuid.UUID) error
}

// AcceptAnyCategory accepts every well-formed category id.
//
// In force until the Category module exists. Named rather than nil so an unwired
// verifier is not mistaken for a permissive one.
type AcceptAnyCategory struct{}

var _ CategoryVerifier = (*AcceptAnyCategory)(nil)

// NewAcceptAnyCategory builds the permissive verifier.
func NewAcceptAnyCategory() *AcceptAnyCategory { return &AcceptAnyCategory{} }

// VerifyCategory always accepts.
func (AcceptAnyCategory) VerifyCategory(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

// ---------- BrandVerifier ----------

// BrandVerifier confirms a brand exists and belongs to a company.
//
// Same reasoning as CategoryVerifier: a brand is another aggregate, so the
// existence check is a verifier the Brand sprint implements rather than a rule
// the product aggregate can enforce.
type BrandVerifier interface {
	// VerifyBrand returns nil when the brand exists in the company.
	VerifyBrand(ctx context.Context, companyID, brandID uuid.UUID) error
}

// AcceptAnyBrand accepts every well-formed brand id. In force until the Brand
// module exists.
type AcceptAnyBrand struct{}

var _ BrandVerifier = (*AcceptAnyBrand)(nil)

// NewAcceptAnyBrand builds the permissive verifier.
func NewAcceptAnyBrand() *AcceptAnyBrand { return &AcceptAnyBrand{} }

// VerifyBrand always accepts.
func (AcceptAnyBrand) VerifyBrand(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------- UOMVerifier ----------

// UOMVerifier confirms a unit of measure exists and belongs to a company.
//
// The product aggregate provisions a base unit at creation and accepts alternate
// units through AddUOM, validating the conversion factor. It cannot validate
// that the unit id refers to a real unit, because units belong to the future UOM
// aggregate. The service verifies both the base unit (at create) and every
// alternate unit (at AddUOM) through this interface.
//
// This is the verifier most likely to gain a real implementation soon, because
// a product with a base unit that does not exist is not merely a dangling
// reference — every quantity in the system is expressed in it.
type UOMVerifier interface {
	// VerifyUOM returns nil when the unit of measure exists in the company.
	VerifyUOM(ctx context.Context, companyID, uomID uuid.UUID) error
}

// AcceptAnyUOM accepts every well-formed unit id. In force until the UOM module
// exists.
type AcceptAnyUOM struct{}

var _ UOMVerifier = (*AcceptAnyUOM)(nil)

// NewAcceptAnyUOM builds the permissive verifier.
func NewAcceptAnyUOM() *AcceptAnyUOM { return &AcceptAnyUOM{} }

// VerifyUOM always accepts.
func (AcceptAnyUOM) VerifyUOM(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// ---------- InventoryProvider ----------

// InventoryProvider reports whether stock exists for a product.
//
// # Why the aggregate cannot answer this
//
// entity.Product.SetTracking refuses to change the tracking method "while
// inventory exists" — changing NONE→SERIAL once there is on-hand stock would
// leave existing units without the serials the new method requires. But whether
// stock exists is a fact about the Inventory aggregate, which the product cannot
// see. So SetTracking takes an `inventoryExists bool` PARAMETER, and the service
// fetches that fact here and passes it in. The RULE stays in the domain; only
// the FACT comes from outside — which is what keeps the rule unit-testable with
// no infrastructure.
//
// # What the Inventory sprint does
//
// It implements this — an existence check over stock rows for the product — and
// bootstrap injects it in place of NoInventory.
type InventoryProvider interface {
	// HasInventory reports whether any stock exists for the product.
	HasInventory(ctx context.Context, companyID, productID uuid.UUID) (bool, error)
}

// NoInventory reports every product as holding no stock.
//
// In force until Inventory exists, and TRUTHFUL rather than merely permissive:
// with no stock module, no product CAN have inventory, so "false" is the fact,
// not a guess. Reporting false means tracking changes are freely allowed today —
// which is correct, because there is no stock a change could strand.
type NoInventory struct{}

var _ InventoryProvider = (*NoInventory)(nil)

// NewNoInventory builds the default provider.
func NewNoInventory() *NoInventory { return &NoInventory{} }

// HasInventory always reports false.
func (NoInventory) HasInventory(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
