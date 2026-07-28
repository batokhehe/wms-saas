package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// A SPECIFICATION is a named business rule evaluated against the whole company
// set — the kind of rule an aggregate cannot check because it can see only
// itself. Each is a small object over the repository that returns nil when
// satisfied or a typed CONFLICT when not.
//
// They are first-class types rather than inline `if taken` blocks in the service
// for three reasons: the rule has a NAME the reviewer and the error message
// share; the same rule is reused across create and update without being
// re-derived; and the intent ("SKUs are unique per company") is stated once, at
// the point the check is defined, rather than implied by a scattered query.
//
// The uniqueness they enforce is ALSO backed by a database index (docs/Product.md
// §4). The specification produces the clear, early message for the common case;
// the index is the race-proof backstop for two requests that pass the check
// concurrently and then both insert.

// UniqueSKU enforces "a SKU identifies at most one live product per company".
type UniqueSKU struct{ products repository.Repository }

// NewUniqueSKU builds the specification.
func NewUniqueSKU(products repository.Repository) UniqueSKU {
	return UniqueSKU{products: products}
}

// Satisfy returns CONFLICT when the SKU is already taken in the company.
func (s UniqueSKU) Satisfy(ctx context.Context, companyID uuid.UUID, sku string) error {
	taken, err := s.products.ExistsBySKU(ctx, companyID, sku)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A product with this SKU already exists").
			WithOp("product.spec.UniqueSKU")
	}
	return nil
}

// UniqueProductName enforces "a product name identifies at most one live product
// per company". A product is chosen from a catalogue search by name, so two
// distinct articles with the same name make mis-picking a matter of chance.
type UniqueProductName struct{ products repository.Repository }

// NewUniqueProductName builds the specification.
func NewUniqueProductName(products repository.Repository) UniqueProductName {
	return UniqueProductName{products: products}
}

// Satisfy returns CONFLICT when the name is taken by another product.
//
// excludeID is the product being renamed, so renaming one to its own current
// name is not a conflict. uuid.Nil means "exclude nothing" — the create case.
func (s UniqueProductName) Satisfy(
	ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) error {
	var (
		taken bool
		err   error
	)
	if excludeID == uuid.Nil {
		taken, err = s.products.ExistsByName(ctx, companyID, name)
	} else {
		taken, err = s.products.ExistsByNameExcluding(ctx, companyID, name, excludeID)
	}
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A product with this name already exists").
			WithOp("product.spec.UniqueProductName")
	}
	return nil
}

// UniqueBarcode enforces "a barcode resolves to at most one product per
// company". This is the scanner-correctness rule: a scan must identify exactly
// one article, so the same barcode on two products would make every scan
// ambiguous.
type UniqueBarcode struct{ products repository.Repository }

// NewUniqueBarcode builds the specification.
func NewUniqueBarcode(products repository.Repository) UniqueBarcode {
	return UniqueBarcode{products: products}
}

// Satisfy returns CONFLICT when the barcode already belongs to another product.
//
// excludeProductID is the product the barcode is being added to, so a barcode
// already persisted on that same product does not report as a conflict against
// itself. The aggregate separately rejects adding a barcode a product already
// holds, so this specification is only concerned with cross-product collisions.
func (s UniqueBarcode) Satisfy(
	ctx context.Context, companyID uuid.UUID, barcode string, excludeProductID uuid.UUID,
) error {
	taken, err := s.products.ExistsByBarcodeExcluding(ctx, companyID, barcode, excludeProductID)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A product with this barcode already exists").
			WithOp("product.spec.UniqueBarcode")
	}
	return nil
}

// Specifications bundles the three rules the service enforces, so the service
// holds one collaborator rather than three and the wiring names the set once.
type Specifications struct {
	UniqueSKU         UniqueSKU
	UniqueProductName UniqueProductName
	UniqueBarcode     UniqueBarcode
}

// NewSpecifications builds the bundle over one repository.
func NewSpecifications(products repository.Repository) Specifications {
	return Specifications{
		UniqueSKU:         NewUniqueSKU(products),
		UniqueProductName: NewUniqueProductName(products),
		UniqueBarcode:     NewUniqueBarcode(products),
	}
}
