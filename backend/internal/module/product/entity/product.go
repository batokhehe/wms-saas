package entity

import (
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
	"time"
)

type ProductBarcode struct {
	barcode Barcode
	primary bool
}

func (v ProductBarcode) Barcode() Barcode { return v.barcode }
func (v ProductBarcode) IsPrimary() bool  { return v.primary }

type ProductUOM struct {
	uomID  uuid.UUID
	factor ConversionFactor
}

func (v ProductUOM) UOMID() uuid.UUID                   { return v.uomID }
func (v ProductUOM) ConversionFactor() ConversionFactor { return v.factor }

// ReconstituteBarcode and ReconstituteUOM rebuild a child-collection element
// from persisted state. They are the PERSISTENCE SEAM for the two collections.
//
// # Why they are required
//
// Reconstitute takes []ProductBarcode and []ProductUOM, but both element types
// have unexported fields and no other exported constructor — so no code outside
// this package can build the slices Reconstitute needs. A repository lives in
// its own package (GORM must stay out of the domain), which left the public
// Reconstitute function uncallable from any real persistence layer: a latent
// bug that only surfaces the moment someone tries to load a product from a row.
//
// These close that gap without touching a single invariant. They perform no
// validation of their own on purpose: Reconstitute.validate() is the one place
// that vets a whole persisted aggregate (exactly one primary barcode, the base
// UOM present with factor one, no duplicates), and duplicating those checks per
// element would let the two drift. An element built here is inert until
// Reconstitute accepts the assembled set.
//
// They are NOT a mutation path. AddBarcode/AddUOM remain the only way to grow a
// LIVE aggregate, and they still enforce every rule. These two exist solely to
// turn stored rows back into value objects the aggregate already trusts.
func ReconstituteBarcode(barcode Barcode, primary bool) ProductBarcode {
	return ProductBarcode{barcode: barcode, primary: primary}
}

// ReconstituteUOM rebuilds one alternate-unit entry from a persisted row. See
// ReconstituteBarcode for why this seam exists.
func ReconstituteUOM(uomID uuid.UUID, factor ConversionFactor) ProductUOM {
	return ProductUOM{uomID: uomID, factor: factor}
}

// Product owns catalog identity and representation. It deliberately does not
// own stock, lots or serials; those are independently mutable Inventory roots.
type Product struct {
	id, companyID        uuid.UUID
	version              uint64
	sku                  SKU
	name                 ProductName
	description          string
	categoryID, brandID  *uuid.UUID
	baseUOMID            uuid.UUID
	status               Status
	tracking             TrackingMethod
	shelfLife            ShelfLife
	weight               *Weight
	dimension            *Dimension
	volume               *Volume
	barcodes             []ProductBarcode
	uoms                 []ProductUOM
	createdBy, updatedBy uuid.UUID
	createdAt, updatedAt time.Time
	events               []Event
}

func NewProduct(id, companyID uuid.UUID, sku SKU, name ProductName, description string, baseUOMID, actor uuid.UUID, now time.Time) (*Product, error) {
	if id == uuid.Nil || companyID == uuid.Nil || actor == uuid.Nil || baseUOMID == uuid.Nil {
		return nil, apperror.Validation("product, company, actor and base uom ids are required")
	}
	p := &Product{id: id, companyID: companyID, version: 1, sku: sku, name: name, description: description, baseUOMID: baseUOMID, status: StatusDraft, tracking: TrackingNone, createdBy: actor, updatedBy: actor, createdAt: now, updatedAt: now, uoms: []ProductUOM{{baseUOMID, MustConversionFactor("1")}}}
	if err := p.validate(); err != nil {
		return nil, err
	}
	// The creation event carries the identifying facts a downstream read model
	// needs to materialise a product row without loading the aggregate: SKU,
	// name, the base unit it is measured in, and the lifecycle and tracking it
	// begins with.
	p.record(EventProductCreated, actor, now, map[string]any{
		"sku":         sku.String(),
		"name":        name.String(),
		"base_uom_id": baseUOMID.String(),
		"status":      p.status.String(),
		"tracking":    p.tracking.String(),
	})
	return p, nil
}

// Reconstitute restores stored state without raising events. Version is read
// only in the domain; repositories use it as their expected lock token.
func Reconstitute(id, companyID uuid.UUID, version uint64, sku SKU, name ProductName, description string, categoryID, brandID *uuid.UUID, baseUOMID uuid.UUID, status Status, tracking TrackingMethod, shelfLife ShelfLife, weight *Weight, dimension *Dimension, volume *Volume, barcodes []ProductBarcode, uoms []ProductUOM, createdBy, updatedBy uuid.UUID, createdAt, updatedAt time.Time) (*Product, error) {
	if version == 0 || !status.Valid() || !tracking.Valid() || id == uuid.Nil || companyID == uuid.Nil || baseUOMID == uuid.Nil {
		return nil, apperror.Validation("invalid persisted product state")
	}
	p := &Product{id: id, companyID: companyID, version: version, sku: sku, name: name, description: description, categoryID: copyID(categoryID), brandID: copyID(brandID), baseUOMID: baseUOMID, status: status, tracking: tracking, shelfLife: shelfLife, weight: copyWeight(weight), dimension: copyDimension(dimension), volume: copyVolume(volume), barcodes: append([]ProductBarcode(nil), barcodes...), uoms: append([]ProductUOM(nil), uoms...), createdBy: createdBy, updatedBy: updatedBy, createdAt: createdAt, updatedAt: updatedAt}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}
func MustConversionFactor(v string) ConversionFactor {
	f, e := NewConversionFactor(v)
	if e != nil {
		panic(e)
	}
	return f
}
func copyID(v *uuid.UUID) *uuid.UUID {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func copyWeight(v *Weight) *Weight {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func copyDimension(v *Dimension) *Dimension {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func copyVolume(v *Volume) *Volume {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
func (p *Product) validate() error {
	if p.sku == "" || p.name == "" {
		return apperror.Validation("product sku and name are required")
	}
	base := false
	primary := 0
	seenBarcode := map[Barcode]bool{}
	seenUOM := map[uuid.UUID]bool{}
	for _, b := range p.barcodes {
		if b.barcode == "" || seenBarcode[b.barcode] {
			return apperror.Validation("invalid product barcode set")
		}
		seenBarcode[b.barcode] = true
		if b.primary {
			primary++
		}
	}
	if len(p.barcodes) > 0 && primary != 1 {
		return apperror.Validation("products with barcodes require exactly one primary barcode")
	}
	for _, u := range p.uoms {
		if u.uomID == uuid.Nil || !u.factor.Decimal().positive() || seenUOM[u.uomID] {
			return apperror.Validation("invalid product uom set")
		}
		seenUOM[u.uomID] = true
		if u.uomID == p.baseUOMID {
			base = true
			if u.factor.Decimal().String() != "1" {
				return apperror.Validation("base uom conversion factor must equal one")
			}
		}
	}
	if !base {
		return apperror.Validation("base uom must exist")
	}
	return nil
}
func (p *Product) ID() uuid.UUID                  { return p.id }
func (p *Product) CompanyID() uuid.UUID           { return p.companyID }
func (p *Product) Version() uint64                { return p.version }
func (p *Product) SKU() SKU                       { return p.sku }
func (p *Product) Name() ProductName              { return p.name }
func (p *Product) Status() Status                 { return p.status }
func (p *Product) TrackingMethod() TrackingMethod { return p.tracking }
func (p *Product) Barcodes() []ProductBarcode     { return append([]ProductBarcode(nil), p.barcodes...) }
func (p *Product) UOMs() []ProductUOM             { return append([]ProductUOM(nil), p.uoms...) }
func (p *Product) ShelfLife() ShelfLife           { return p.shelfLife }
func (p *Product) Weight() *Weight                { return copyWeight(p.weight) }
func (p *Product) Dimension() *Dimension          { return copyDimension(p.dimension) }
func (p *Product) Volume() *Volume                { return copyVolume(p.volume) }

// Read-only accessors for persisted state that has no other getter.
//
// These are the READ half of the persistence seam (ReconstituteBarcode/UOM are
// the write half). toModel must be able to read every field the row stores, and
// these fields — description, the three unit/taxonomy references, and the audit
// columns — were reachable only for writing (ChangeDescription, AssignCategory,
// the actor/now arguments) and not for reading. Without them the aggregate is
// not round-trippable: a save would silently drop the description, the base
// unit and the audit trail, which no compiler would catch.
//
// They are getters over existing fields: no invariant, value object, child
// collection or event is touched. CategoryID/BrandID return copies so a caller
// cannot mutate the aggregate's pointer, exactly as Weight/Dimension/Volume do.
func (p *Product) Description() string    { return p.description }
func (p *Product) BaseUOMID() uuid.UUID   { return p.baseUOMID }
func (p *Product) CategoryID() *uuid.UUID { return copyID(p.categoryID) }
func (p *Product) BrandID() *uuid.UUID    { return copyID(p.brandID) }
func (p *Product) CreatedBy() uuid.UUID   { return p.createdBy }
func (p *Product) UpdatedBy() uuid.UUID   { return p.updatedBy }
func (p *Product) CreatedAt() time.Time   { return p.createdAt }
func (p *Product) UpdatedAt() time.Time   { return p.updatedAt }

func (p *Product) touch(actor uuid.UUID, now time.Time) { p.updatedBy = actor; p.updatedAt = now }

// State machine: DRAFT→ACTIVE, ACTIVE→DRAFT, DRAFT|ACTIVE→DISCONTINUED;
// DISCONTINUED is terminal.
func (p *Product) Activate(actor uuid.UUID, now time.Time) error {
	if p.status == StatusDiscontinued {
		return apperror.Conflict("discontinued product cannot be activated")
	}
	if p.status == StatusActive {
		return nil
	}
	p.status = StatusActive
	p.touch(actor, now)
	p.record(EventProductActivated, actor, now, map[string]any{"from": StatusDraft.String(), "to": StatusActive.String()})
	return nil
}
func (p *Product) Deactivate(actor uuid.UUID, now time.Time) error {
	if p.status == StatusDiscontinued {
		return apperror.Conflict("discontinued product cannot be deactivated")
	}
	if p.status == StatusDraft {
		return nil
	}
	p.status = StatusDraft
	p.touch(actor, now)
	p.record(EventProductDeactivated, actor, now, map[string]any{"from": StatusActive.String(), "to": StatusDraft.String()})
	return nil
}
func (p *Product) Discontinue(actor uuid.UUID, now time.Time) error {
	if p.status == StatusDiscontinued {
		return apperror.Conflict("product is already discontinued")
	}
	old := p.status
	p.status = StatusDiscontinued
	p.touch(actor, now)
	p.record(EventProductDiscontinued, actor, now, map[string]any{"from": old.String(), "to": StatusDiscontinued.String()})
	return nil
}
func (p *Product) Rename(name ProductName, actor uuid.UUID, now time.Time) error {
	if p.status == StatusActive {
		return apperror.Conflict("active product name cannot change")
	}
	if name == p.name {
		return nil
	}
	old := p.name
	p.name = name
	p.touch(actor, now)
	p.record(EventProductRenamed, actor, now, map[string]any{"old": old.String(), "new": name.String()})
	return nil
}
func (p *Product) ChangeDescription(v string, actor uuid.UUID, now time.Time) {
	p.description = v
	p.touch(actor, now)
}
func (p *Product) SetShelfLife(value ShelfLife, actor uuid.UUID, now time.Time) {
	old := p.shelfLife
	if old == value {
		return
	}
	p.shelfLife = value
	p.touch(actor, now)
	p.record(EventShelfLifeChanged, actor, now, map[string]any{
		"old_defined": old.IsDefined(), "old_days": old.Days(),
		"new_defined": value.IsDefined(), "new_days": value.Days(),
	})
}

// SetMeasurements replaces the physical profile. Any non-nil measurement must
// be a constructed, positive value object; a caller can build a *Weight or
// *Dimension from struct zero values that never passed a constructor, and
// without this guard those unvalidated measurements would enter the aggregate.
//
// Validation happens for all three BEFORE any assignment, so a single invalid
// side cannot leave the product with a partially-updated profile.
func (p *Product) SetMeasurements(weight *Weight, dimension *Dimension, volume *Volume, actor uuid.UUID, now time.Time) error {
	if weight != nil && !weight.IsSet() {
		return apperror.Validation("weight must be a positive measurement")
	}
	if volume != nil && !volume.IsSet() {
		return apperror.Validation("volume must be a positive measurement")
	}
	if dimension != nil && !dimension.IsValid() {
		return apperror.Validation("dimension must have positive width, height and length")
	}
	p.weight, p.dimension, p.volume = copyWeight(weight), copyDimension(dimension), copyVolume(volume)
	p.touch(actor, now)
	p.record(EventMeasurementsChanged, actor, now, map[string]any{
		"has_weight":    weight != nil,
		"has_dimension": dimension != nil,
		"has_volume":    volume != nil,
	})
	return nil
}
func (p *Product) AssignCategory(id *uuid.UUID, actor uuid.UUID, now time.Time) error {
	if id != nil && *id == uuid.Nil {
		return apperror.Validation("category id is invalid")
	}
	old := idString(p.categoryID)
	p.categoryID = copyID(id)
	p.touch(actor, now)
	p.record(EventCategoryAssigned, actor, now, map[string]any{"old": old, "new": idString(id)})
	return nil
}
func (p *Product) AssignBrand(id *uuid.UUID, actor uuid.UUID, now time.Time) error {
	if id != nil && *id == uuid.Nil {
		return apperror.Validation("brand id is invalid")
	}
	old := idString(p.brandID)
	p.brandID = copyID(id)
	p.touch(actor, now)
	p.record(EventBrandAssigned, actor, now, map[string]any{"old": old, "new": idString(id)})
	return nil
}
func (p *Product) AddBarcode(b Barcode, primary bool, actor uuid.UUID, now time.Time) error {
	for _, x := range p.barcodes {
		if x.barcode == b {
			return apperror.Conflict("barcode already belongs to product")
		}
	}
	if len(p.barcodes) == 0 {
		primary = true
	}
	if primary {
		for i := range p.barcodes {
			p.barcodes[i].primary = false
		}
	}
	p.barcodes = append(p.barcodes, ProductBarcode{b, primary})
	p.touch(actor, now)
	p.record(EventBarcodeAdded, actor, now, map[string]any{"barcode": b.String(), "primary": primary})
	return nil
}
func (p *Product) SetPrimaryBarcode(b Barcode, actor uuid.UUID, now time.Time) error {
	found := false
	for _, x := range p.barcodes {
		if x.barcode == b {
			found = true
			break
		}
	}
	if !found {
		return apperror.NotFound("barcode was not assigned to product")
	}
	old := ""
	for i := range p.barcodes {
		if p.barcodes[i].primary {
			old = p.barcodes[i].barcode.String()
		}
		p.barcodes[i].primary = p.barcodes[i].barcode == b
	}
	p.touch(actor, now)
	p.record(EventPrimaryBarcodeChanged, actor, now, map[string]any{"old": old, "new": b.String()})
	return nil
}
func (p *Product) RemoveBarcode(b Barcode, actor uuid.UUID, now time.Time) error {
	for i, x := range p.barcodes {
		if x.barcode == b {
			if x.primary && len(p.barcodes) > 1 {
				return apperror.Conflict("assign another primary barcode first")
			}
			wasPrimary := x.primary
			p.barcodes = append(p.barcodes[:i], p.barcodes[i+1:]...)
			p.touch(actor, now)
			p.record(EventBarcodeRemoved, actor, now, map[string]any{"barcode": b.String(), "primary": wasPrimary})
			return nil
		}
	}
	return apperror.NotFound("barcode was not assigned to product")
}
func (p *Product) AddUOM(id uuid.UUID, factor ConversionFactor, actor uuid.UUID, now time.Time) error {
	if id == uuid.Nil {
		return apperror.Validation("uom id is required")
	}
	// The factor is re-validated here, not assumed valid. A caller can pass a
	// ConversionFactor{} struct zero value that never went through
	// NewConversionFactor; without this guard that non-positive factor would
	// enter the UOM set and corrupt every quantity conversion derived from it.
	if !factor.IsValid() {
		return apperror.Validation("conversion factor must be a positive decimal")
	}
	if id == p.baseUOMID {
		// The base UOM is provisioned at construction with factor 1 and is the
		// unit every alternate factor is expressed against. Re-adding it — even
		// with factor 1 — is rejected so "the base unit" stays singular and
		// unambiguous.
		return apperror.Conflict("base uom already belongs to product")
	}
	for _, u := range p.uoms {
		if u.uomID == id {
			return apperror.Conflict("uom already belongs to product")
		}
	}
	p.uoms = append(p.uoms, ProductUOM{id, factor})
	p.touch(actor, now)
	p.record(EventUOMAdded, actor, now, map[string]any{
		"uom_id": id.String(), "factor": factor.Decimal().String(),
	})
	return nil
}
func (p *Product) RemoveUOM(id uuid.UUID, actor uuid.UUID, now time.Time) error {
	if id == p.baseUOMID {
		return apperror.Conflict("base uom cannot be removed")
	}
	for i, u := range p.uoms {
		if u.uomID == id {
			factor := u.factor
			p.uoms = append(p.uoms[:i], p.uoms[i+1:]...)
			p.touch(actor, now)
			p.record(EventUOMRemoved, actor, now, map[string]any{
				"uom_id": id.String(), "factor": factor.Decimal().String(),
			})
			return nil
		}
	}
	return apperror.NotFound("uom was not assigned to product")
}
func (p *Product) SetTracking(method TrackingMethod, inventoryExists bool, actor uuid.UUID, now time.Time) error {
	if !method.Valid() {
		return apperror.Validation("invalid tracking method")
	}
	if inventoryExists && method != p.tracking {
		return apperror.Conflict("tracking method cannot change while inventory exists")
	}
	if method != p.tracking {
		old := p.tracking
		p.tracking = method
		p.touch(actor, now)
		p.record(EventTrackingMethodChanged, actor, now, map[string]any{"old": old.String(), "new": method.String()})
	}
	return nil
}
func (p *Product) EnableLotTracking(hasInventory bool, actor uuid.UUID, now time.Time) error {
	return p.SetTracking(TrackingLot, hasInventory, actor, now)
}
func (p *Product) EnableSerialTracking(hasInventory bool, actor uuid.UUID, now time.Time) error {
	return p.SetTracking(TrackingSerial, hasInventory, actor, now)
}
func (p *Product) DisableTracking(hasInventory bool, actor uuid.UUID, now time.Time) error {
	return p.SetTracking(TrackingNone, hasInventory, actor, now)
}
