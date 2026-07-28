// Package dto holds the product module's transport contracts.
//
// LAYER RULE: DTOs are never entities and entities are never DTOs. Here that is
// especially clear — entity.Product has no exported fields at all, so a DTO
// could not double as one even if someone wanted it to.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Requests ----------

// CreateProductRequest registers a product.
//
// Status and tracking are deliberately absent: a product is always created in
// DRAFT with tracking NONE, and reaches other states only through the lifecycle
// and tracking endpoints, which enforce their rules. Letting a client choose the
// initial status would be a way to bypass them.
//
// Measurements, shelf life, category, brand, barcodes and alternate units are
// absent for a different reason: each is supplied through its own
// intent-revealing operation. Accepting them here would make Create a
// general-purpose setter, which is exactly the CRUD shape this module is not.
type CreateProductRequest struct {
	SKU         string    `json:"sku"         binding:"required,min=1,max=64"`
	Name        string    `json:"name"        binding:"required,min=2,max=255"`
	Description string    `json:"description" binding:"omitempty,max=2000"`
	BaseUOMID   uuid.UUID `json:"base_uom_id" binding:"required"`
}

// UpdateProductRequest applies a partial attribute update.
//
// Pointer fields distinguish "field omitted" from "field set to empty". SKU is
// NOT updatable: it is scanned, printed on pick lists and spoken over a radio,
// so changing it invalidates physical artefacts the system does not control.
// Status and tracking are not here either — they go through their own endpoints,
// which is what keeps the state machine in the aggregate.
type UpdateProductRequest struct {
	Name        *string `json:"name"        binding:"omitempty,min=2,max=255"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
}

// AssignCategoryRequest sets or clears the category. A null category_id clears
// it; a value assigns it (and is verified to exist).
type AssignCategoryRequest struct {
	CategoryID *uuid.UUID `json:"category_id"`
}

// AssignBrandRequest sets or clears the brand.
type AssignBrandRequest struct {
	BrandID *uuid.UUID `json:"brand_id"`
}

// DimensionInput is the structured box dimension. All three sides are required
// together — a box with a width and height but no length describes nothing
// physical, and the aggregate rejects it.
type DimensionInput struct {
	Width  string `json:"width"  binding:"required"`
	Height string `json:"height" binding:"required"`
	Length string `json:"length" binding:"required"`
}

// SetMeasurementsRequest replaces the physical profile. Every field is optional;
// an omitted field clears that measurement. Values are decimal strings, not
// numbers, because the domain stores exact rationals and a JSON number is an
// IEEE-754 float that would lose "1/3" on the wire. See docs/Product.md §6.
type SetMeasurementsRequest struct {
	Weight    *string         `json:"weight"    binding:"omitempty"`
	Volume    *string         `json:"volume"    binding:"omitempty"`
	Dimension *DimensionInput `json:"dimension" binding:"omitempty"`
}

// SetShelfLifeRequest sets or clears the shelf life. A null days clears it
// (undefined); a value — including 0 — defines it. Zero is meaningful: a product
// that expires on manufacture.
type SetShelfLifeRequest struct {
	Days *int `json:"days" binding:"omitempty,min=0"`
}

// SetTrackingRequest changes the tracking method.
type SetTrackingRequest struct {
	Method string `json:"method" binding:"required,oneof=NONE LOT SERIAL"`
}

// AddBarcodeRequest assigns a barcode. Primary requests that this barcode become
// the primary one; the first barcode is always primary regardless.
type AddBarcodeRequest struct {
	Barcode string `json:"barcode" binding:"required,min=1,max=64"`
	Primary bool   `json:"primary"`
}

// SetPrimaryBarcodeRequest promotes an existing barcode to primary.
type SetPrimaryBarcodeRequest struct {
	Barcode string `json:"barcode" binding:"required,min=1,max=64"`
}

// AddUOMRequest adds an alternate unit of measure with its conversion factor to
// the base unit. Factor is a decimal string for the exactness reason above.
type AddUOMRequest struct {
	UOMID  uuid.UUID `json:"uom_id" binding:"required"`
	Factor string    `json:"factor" binding:"required"`
}

// ListProductsQuery is the list endpoint's query string.
type ListProductsQuery struct {
	pagination.Request

	Status   string `form:"status"   binding:"omitempty,oneof=DRAFT ACTIVE DISCONTINUED"`
	Tracking string `form:"tracking" binding:"omitempty,oneof=NONE LOT SERIAL"`
}

// SortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control: ORDER BY cannot be parameterised by any
// SQL driver, so the column name is interpolated. Only keys listed here can ever
// reach the database.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 25,
		MaxLimit:     100,
		DefaultSort:  "sku",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{
			"sku":        "products.sku",
			"name":       "products.name",
			"status":     "products.status",
			"created_at": "products.created_at",
			"updated_at": "products.updated_at",
		},
	}
}

// IDParam binds a UUID path parameter.
//
// The field is a STRING, not a uuid.UUID. Gin's URI binder maps path segments by
// reflection over basic kinds; uuid.UUID is a [16]byte array and the binder
// rejects it, producing a 400 on every request including well-formed ones.
type IDParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

// UUID returns the parsed identifier.
func (p IDParam) UUID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field:   "id",
			Rule:    "uuid",
			Message: "id must be a valid UUID",
		}).WithOp("product.dto.IDParam.UUID").WithCause(err)
	}
	return parsed, nil
}

// UOMParam binds the product id and the unit id from a nested path,
// /products/:id/uoms/:uomId.
type UOMParam struct {
	ID    string `uri:"id"    binding:"required,uuid"`
	UOMID string `uri:"uomId" binding:"required,uuid"`
}

// IDs returns the parsed product and unit identifiers.
func (p UOMParam) IDs() (productID, uomID uuid.UUID, err error) {
	productID, err = uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field: "id", Rule: "uuid", Message: "id must be a valid UUID",
		}).WithOp("product.dto.UOMParam.IDs").WithCause(err)
	}
	uomID, err = uuid.Parse(p.UOMID)
	if err != nil {
		return uuid.Nil, uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field: "uomId", Rule: "uuid", Message: "uomId must be a valid UUID",
		}).WithOp("product.dto.UOMParam.IDs").WithCause(err)
	}
	return productID, uomID, nil
}

// BarcodeParam binds the product id and the barcode from a nested path,
// /products/:id/barcodes/:barcode. The barcode is a free-form string, not a
// UUID, so it is validated for length rather than format.
type BarcodeParam struct {
	ID      string `uri:"id"      binding:"required,uuid"`
	Barcode string `uri:"barcode" binding:"required,min=1,max=64"`
}

// ProductID returns the parsed product identifier.
func (p BarcodeParam) ProductID() (uuid.UUID, error) {
	parsed, err := uuid.Parse(p.ID)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field: "id", Rule: "uuid", Message: "id must be a valid UUID",
		}).WithOp("product.dto.BarcodeParam.ProductID").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// BarcodeResponse is one barcode in a product's response.
type BarcodeResponse struct {
	Barcode   string `json:"barcode"`
	IsPrimary bool   `json:"is_primary"`
}

// UOMResponse is one alternate unit in a product's response.
type UOMResponse struct {
	UOMID  uuid.UUID `json:"uom_id"`
	Factor string    `json:"factor"`
	IsBase bool      `json:"is_base"`
}

// DimensionResponse reports a product's box dimension as exact decimal strings.
type DimensionResponse struct {
	Width  string `json:"width"`
	Height string `json:"height"`
	Length string `json:"length"`
}

// ProductResponse is the public representation of a product.
//
// Measurements and shelf life are pointers so an unmeasured product omits them
// rather than reporting a misleading "0". ShelfLifeDays is reported alongside a
// boolean so a defined zero-day shelf life is distinguishable from an undefined
// one — the same distinction the aggregate makes.
type ProductResponse struct {
	ID          uuid.UUID `json:"id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`

	CategoryID *uuid.UUID `json:"category_id,omitempty"`
	BrandID    *uuid.UUID `json:"brand_id,omitempty"`
	BaseUOMID  uuid.UUID  `json:"base_uom_id"`

	Status   string `json:"status"`
	Tracking string `json:"tracking"`

	ShelfLifeDefined bool `json:"shelf_life_defined"`
	ShelfLifeDays    *int `json:"shelf_life_days,omitempty"`

	Weight    *string            `json:"weight,omitempty"`
	Volume    *string            `json:"volume,omitempty"`
	Dimension *DimensionResponse `json:"dimension,omitempty"`

	Barcodes []BarcodeResponse `json:"barcodes"`
	UOMs     []UOMResponse     `json:"uoms"`

	CreatedBy uuid.UUID `json:"created_by"`
	UpdatedBy uuid.UUID `json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
