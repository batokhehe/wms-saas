package entity

import (
	"strings"
	"time"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
	"github.com/google/uuid"
)

type Status string

const (
	StatusActive   Status = "ACTIVE"
	StatusInactive Status = "INACTIVE"
)

func (s Status) Valid() bool    { return s == StatusActive || s == StatusInactive }
func (s Status) String() string { return string(s) }

type Product struct {
	id, companyID, categoryID, brandID, baseUOMID uuid.UUID
	sku                                           SKU
	name                                          ProductName
	description                                   string
	trackingConfig                                TrackingConfig
	inventoryPolicy                               InventoryPolicy
	dimensions                                    Dimensions
	weight                                        Weight
	volume                                        Volume
	status                                        Status
	createdAt, updatedAt                          time.Time
	deletedAt                                     *time.Time
	version                                       uint64
	barcodes                                      []ProductBarcode
	uoms                                          []AlternateUOM
}

func New(id, companyID, categoryID, brandID, baseUOMID uuid.UUID, sku SKU, name ProductName, description string, trackingConfig TrackingConfig, inventoryPolicy InventoryPolicy, dimensions Dimensions, weight Weight, volume Volume, now time.Time) (*Product, error) {
	p := &Product{
		id: id, companyID: companyID, categoryID: categoryID, brandID: brandID, baseUOMID: baseUOMID,
		sku: sku, name: name, description: description,
		trackingConfig: trackingConfig, inventoryPolicy: inventoryPolicy,
		dimensions: dimensions, weight: weight, volume: volume,
		status: StatusActive, createdAt: now, updatedAt: now, version: 1,
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func ReconstituteWithVersion(id, companyID, categoryID, brandID, baseUOMID uuid.UUID, sku SKU, name ProductName, description string, trackingConfig TrackingConfig, inventoryPolicy InventoryPolicy, dimensions Dimensions, weight Weight, volume Volume, status Status, createdAt, updatedAt time.Time, deletedAt *time.Time, version uint64) (*Product, error) {
	p := &Product{
		id: id, companyID: companyID, categoryID: categoryID, brandID: brandID, baseUOMID: baseUOMID,
		sku: sku, name: name, description: description,
		trackingConfig: trackingConfig, inventoryPolicy: inventoryPolicy,
		dimensions: dimensions, weight: weight, volume: volume,
		status: status, createdAt: createdAt, updatedAt: updatedAt, deletedAt: deletedAt, version: version,
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Product) validate() error {
	if p.id == uuid.Nil || p.companyID == uuid.Nil || p.categoryID == uuid.Nil || p.brandID == uuid.Nil || p.baseUOMID == uuid.Nil {
		return apperror.Validation("invalid product identifiers")
	}
	return nil
}

func (p *Product) ID() uuid.UUID        { return p.id }
func (p *Product) CompanyID() uuid.UUID { return p.companyID }
func (p *Product) SKU() SKU             { return p.sku }
func (p *Product) Version() uint64      { return p.version }
func (p *Product) Name() ProductName    { return p.name }
func (p *Product) Description() string  { return p.description }
func (p *Product) Status() Status       { return p.status }
func (p *Product) IsArchived() bool     { return p.deletedAt != nil }
func (p *Product) DeletedAt() *time.Time {
	if p.deletedAt == nil {
		return nil
	}
	at := *p.deletedAt
	return &at
}
func (p *Product) CreatedAt() time.Time             { return p.createdAt }
func (p *Product) UpdatedAt() time.Time             { return p.updatedAt }
func (p *Product) TrackingConfig() TrackingConfig   { return p.trackingConfig }
func (p *Product) InventoryPolicy() InventoryPolicy { return p.inventoryPolicy }
func (p *Product) Dimensions() Dimensions           { return p.dimensions }
func (p *Product) Weight() Weight                   { return p.weight }
func (p *Product) Volume() Volume                   { return p.volume }

func (p *Product) UpdateDetails(name ProductName, description string, now time.Time) error {
	if p.IsArchived() {
		return apperror.Conflict("cannot update archived product")
	}
	p.name = name
	p.description = description
	p.touch(now)
	return nil
}

func (p *Product) UpdateTrackingConfig(newConfig TrackingConfig, verifier StockVerifier) error {
	if p.IsArchived() {
		return apperror.Conflict("cannot update archived product")
	}
	hasMovements, err := verifier.HasMovements(p.id)
	if err != nil {
		return err
	}
	if hasMovements {
		return apperror.Conflict("cannot change tracking config after stock movement")
	}
	p.trackingConfig = newConfig
	return nil
}

func (p *Product) UpdateInventoryPolicy(newPolicy InventoryPolicy, now time.Time) error {
	if p.IsArchived() {
		return apperror.Conflict("cannot update archived product")
	}
	p.inventoryPolicy = newPolicy
	p.touch(now)
	return nil
}

func (p *Product) UpdatePhysicalProperties(dim Dimensions, weight Weight, vol Volume, now time.Time) error {
	if p.IsArchived() {
		return apperror.Conflict("cannot update archived product")
	}
	p.dimensions = dim
	p.weight = weight
	p.volume = vol
	p.touch(now)
	return nil
}

func (p *Product) Activate(now time.Time) {
	if !p.IsArchived() {
		p.status = StatusActive
		p.touch(now)
	}
}

func (p *Product) Deactivate(now time.Time) {
	if !p.IsArchived() {
		p.status = StatusInactive
		p.touch(now)
	}
}

func (p *Product) Archive(now time.Time, verifier InventoryVerifier) error {
	if p.IsArchived() {
		return apperror.Conflict("already archived")
	}
	hasStock, err := verifier.HasStock(p.id)
	if err != nil {
		return err
	}
	if hasStock {
		return apperror.Conflict("cannot archive product with active stock")
	}
	p.deletedAt = &now
	p.touch(now)
	return nil
}

func (p *Product) AddBarcode(id uuid.UUID, code, barcodeType string, isPrimary bool) error {
	for _, b := range p.barcodes {
		if strings.EqualFold(b.Code, code) {
			return apperror.Conflict("duplicate barcode")
		}
	}
	if isPrimary {
		for i := range p.barcodes {
			p.barcodes[i].IsPrimary = false
		}
	}
	p.barcodes = append(p.barcodes, ProductBarcode{ID: id, ProductID: p.id, Code: code, Type: barcodeType, IsPrimary: isPrimary})
	return nil
}

func (p *Product) ReconstituteBarcode(id uuid.UUID, code, barcodeType string, isPrimary bool) {
	p.barcodes = append(p.barcodes, ProductBarcode{ID: id, ProductID: p.id, Code: code, Type: barcodeType, IsPrimary: isPrimary})
}

func (p *Product) Barcodes() []ProductBarcode    { return p.barcodes }
func (p *Product) AlternateUOMs() []AlternateUOM { return p.uoms }

func (p *Product) RemoveBarcode(id uuid.UUID) error {
	for i, b := range p.barcodes {
		if b.ID == id {
			p.barcodes = append(p.barcodes[:i], p.barcodes[i+1:]...)
			return nil
		}
	}
	return apperror.NotFound("barcode not found")
}

func (p *Product) SetPrimaryBarcode(id uuid.UUID) error {
	found := false
	for i := range p.barcodes {
		if p.barcodes[i].ID == id {
			p.barcodes[i].IsPrimary = true
			found = true
		} else {
			p.barcodes[i].IsPrimary = false
		}
	}
	if !found {
		return apperror.NotFound("barcode not found")
	}
	return nil
}

func (p *Product) AddAlternateUOM(id, uomID uuid.UUID, factor float64) error {
	if uomID == p.baseUOMID {
		return apperror.Conflict("cannot add base UOM as alternate")
	}
	for _, u := range p.uoms {
		if u.UOMID == uomID {
			return apperror.Conflict("duplicate alternate UOM")
		}
	}
	p.uoms = append(p.uoms, AlternateUOM{ID: id, ProductID: p.id, UOMID: uomID, Factor: factor})
	return nil
}

func (p *Product) ReconstituteAlternateUOM(id, uomID uuid.UUID, factor float64) {
	p.uoms = append(p.uoms, AlternateUOM{ID: id, ProductID: p.id, UOMID: uomID, Factor: factor})
}

func (p *Product) UpdateAlternateUOM(uomID uuid.UUID, factor float64) error {
	for i := range p.uoms {
		if p.uoms[i].UOMID == uomID {
			p.uoms[i].Factor = factor
			return nil
		}
	}
	return apperror.NotFound("alternate UOM not found")
}

func (p *Product) RemoveAlternateUOM(uomID uuid.UUID) error {
	for i, u := range p.uoms {
		if u.UOMID == uomID {
			p.uoms = append(p.uoms[:i], p.uoms[i+1:]...)
			return nil
		}
	}
	return apperror.NotFound("alternate UOM not found")
}

func (p *Product) touch(now time.Time) { p.updatedAt = now }
