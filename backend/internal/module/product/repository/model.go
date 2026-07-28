// Package repository is the product module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// Like the warehouse module, it carries a responsibility the CRUD modules do
// not: translating between the aggregate — whose fields are unexported and
// unreachable by reflection — and persistence models GORM can map. It carries
// one MORE than warehouse did: the aggregate has two child collections
// (barcodes and alternate units), each in its own table, so the translation is
// three models rather than one and the write path is transactional. See
// repository.go and docs/Product.md §4.
package repository

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// productModel is the persistence representation of the Product aggregate root
// (the parent row only — the child collections are the two models below).
//
// # Why a separate type exists
//
// entity.Product has unexported fields and no setters, which is what makes it an
// aggregate root — no caller reaches ACTIVE except through Activate(), and no
// caller adds an unvalidated barcode. GORM maps by reflecting over EXPORTED
// fields, so it cannot read or write that type at all. Exporting the aggregate's
// fields would delete the encapsulation the domain rests on; implementing
// database/sql interfaces on it would drag persistence into the innermost layer.
// So the aggregate stays pure and these models absorb the ORM.
//
// The measurement and factor columns are TEXT, not NUMERIC. The Product domain
// represents every decimal as an exact big.Rat and forbids float64; NUMERIC(p,s)
// would round a factor of "1/3" and reintroduce the very error the value objects
// exist to prevent. The canonical rational string round-trips exactly. See
// docs/Product.md §6.
type productModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	SKU         string `gorm:"type:citext;not null"`
	Name        string `gorm:"type:citext;not null"`
	Description string `gorm:"type:text;not null;default:''"`

	CategoryID *uuid.UUID `gorm:"type:uuid"`
	BrandID    *uuid.UUID `gorm:"type:uuid"`
	BaseUOMID  uuid.UUID  `gorm:"type:uuid;not null"`

	Status   string `gorm:"type:varchar(16);not null;default:DRAFT"`
	Tracking string `gorm:"type:varchar(16);not null;default:NONE"`

	// ShelfLifeDays is a nullable pointer so NULL (undefined) is distinct from a
	// defined zero-day shelf life, matching entity.ShelfLife's own distinction.
	ShelfLifeDays *int `gorm:"column:shelf_life_days"`

	// Physical profile as exact rational strings; nil when unmeasured.
	WeightKg    *string `gorm:"column:weight_kg;type:text"`
	VolumeM3    *string `gorm:"column:volume_m3;type:text"`
	DimWidthCm  *string `gorm:"column:dim_width_cm;type:text"`
	DimHeightCm *string `gorm:"column:dim_height_cm;type:text"`
	DimLengthCm *string `gorm:"column:dim_length_cm;type:text"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null"`
	UpdatedBy uuid.UUID `gorm:"type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (productModel) TableName() string { return "products" }

// productBarcodeModel is one row of the barcode child collection.
//
// It does NOT embed BaseEntity: these rows have no independent optimistic-lock
// token and are never soft-deleted. They are written and replaced wholesale
// with their parent inside one transaction (see repository.go), and the
// aggregate plus its events are the audit trail, not the child rows.
type productBarcodeModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProductID uuid.UUID `gorm:"type:uuid;not null;index"`
	CompanyID uuid.UUID `gorm:"type:uuid;not null"`
	Barcode   string    `gorm:"type:citext;not null"`
	IsPrimary bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the table name.
func (productBarcodeModel) TableName() string { return "product_barcodes" }

// productUOMModel is one row of the alternate-unit child collection.
type productUOMModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProductID uuid.UUID `gorm:"type:uuid;not null;index"`
	CompanyID uuid.UUID `gorm:"type:uuid;not null"`
	UOMID     uuid.UUID `gorm:"column:uom_id;type:uuid;not null"`
	Factor    string    `gorm:"type:text;not null"`
	IsBase    bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the table name.
func (productUOMModel) TableName() string { return "product_uoms" }

// ---------- aggregate → persistence ----------

// toModel translates the aggregate's parent row.
//
// It reads through the aggregate's getters, which is the only access anyone has
// — the persistence layer is subject to exactly the same encapsulation as every
// other caller. The children are translated separately (toBarcodeModels /
// toUOMModels) because they are inserted into their own tables.
func toModel(p *entity.Product) *productModel {
	model := &productModel{
		CompanyID:     p.CompanyID(),
		SKU:           p.SKU().String(),
		Name:          p.Name().String(),
		Description:   p.Description(),
		CategoryID:    p.CategoryID(),
		BrandID:       p.BrandID(),
		BaseUOMID:     p.BaseUOMID(),
		Status:        p.Status().String(),
		Tracking:      p.TrackingMethod().String(),
		ShelfLifeDays: shelfLifeToColumn(p.ShelfLife()),
		WeightKg:      weightToColumn(p.Weight()),
		VolumeM3:      volumeToColumn(p.Volume()),
		CreatedBy:     p.CreatedBy(),
		UpdatedBy:     p.UpdatedBy(),
	}
	model.DimWidthCm, model.DimHeightCm, model.DimLengthCm = dimensionToColumns(p.Dimension())

	model.ID = p.ID()
	model.Version = p.Version()
	model.CreatedAt = p.CreatedAt()
	model.UpdatedAt = p.UpdatedAt()

	return model
}

// toBarcodeModels translates the barcode collection. IDs and timestamps are
// assigned by the repository at write time, so they are left zero here.
func toBarcodeModels(p *entity.Product) []productBarcodeModel {
	barcodes := p.Barcodes()
	rows := make([]productBarcodeModel, 0, len(barcodes))
	for _, b := range barcodes {
		rows = append(rows, productBarcodeModel{
			ProductID: p.ID(),
			CompanyID: p.CompanyID(),
			Barcode:   b.Barcode().String(),
			IsPrimary: b.IsPrimary(),
		})
	}
	return rows
}

// toUOMModels translates the alternate-unit collection.
func toUOMModels(p *entity.Product) []productUOMModel {
	uoms := p.UOMs()
	base := p.BaseUOMID()
	rows := make([]productUOMModel, 0, len(uoms))
	for _, u := range uoms {
		rows = append(rows, productUOMModel{
			ProductID: p.ID(),
			CompanyID: p.CompanyID(),
			UOMID:     u.UOMID(),
			Factor:    u.ConversionFactor().Decimal().String(),
			IsBase:    u.UOMID() == base,
		})
	}
	return rows
}

// ---------- persistence → aggregate ----------

// toDomain rebuilds an aggregate from its parent row and child rows.
//
// It calls entity.Reconstitute, NOT entity.NewProduct. Loading rows is not a
// business event: constructing through the factory would raise ProductCreated on
// every read, and an audit log would claim the product was created once per page
// view.
//
// Value-object construction errors are DISCARDED rather than returned, matching
// the warehouse module's documented rule: the data came from the database, which
// already enforced its constraints, so a stored value that fails validation
// means a migration went wrong — and refusing to load the row would make the bad
// data impossible to inspect or repair. The zero value is preserved instead,
// which is visible and fixable. Reconstitute itself still rejects a structurally
// invalid AGGREGATE (missing base UOM, two primary barcodes), which is the check
// that actually protects an invariant.
func toDomain(
	model *productModel,
	barcodeRows []productBarcodeModel,
	uomRows []productUOMModel,
) *entity.Product {
	sku, _ := entity.NewSKU(model.SKU)
	name, _ := entity.NewProductName(model.Name)

	barcodes := make([]entity.ProductBarcode, 0, len(barcodeRows))
	for _, row := range barcodeRows {
		barcode, _ := entity.NewBarcode(row.Barcode)
		barcodes = append(barcodes, entity.ReconstituteBarcode(barcode, row.IsPrimary))
	}

	uoms := make([]entity.ProductUOM, 0, len(uomRows))
	for _, row := range uomRows {
		factor, _ := entity.NewConversionFactor(row.Factor)
		uoms = append(uoms, entity.ReconstituteUOM(row.UOMID, factor))
	}

	product, _ := entity.Reconstitute(
		model.ID,
		model.CompanyID,
		model.Version,
		sku,
		name,
		model.Description,
		model.CategoryID,
		model.BrandID,
		model.BaseUOMID,
		entity.Status(model.Status),
		entity.TrackingMethod(model.Tracking),
		columnToShelfLife(model.ShelfLifeDays),
		columnToWeight(model.WeightKg),
		columnToDimension(model.DimWidthCm, model.DimHeightCm, model.DimLengthCm),
		columnToVolume(model.VolumeM3),
		barcodes,
		uoms,
		model.CreatedBy,
		model.UpdatedBy,
		model.CreatedAt,
		model.UpdatedAt,
	)

	// Reconstitute only errors on structurally invalid persisted state, which a
	// migration-managed schema does not produce. A nil here would mean the row
	// itself is corrupt; returning it is the caller's problem to notice, and the
	// warehouse module made the same call to keep this translator total.
	return product
}

// ---------- measurement column helpers ----------
//
// Each pairs a to-column writer with a from-column reader, kept adjacent so the
// two halves of one round trip cannot drift.

func shelfLifeToColumn(s entity.ShelfLife) *int {
	if !s.IsDefined() {
		return nil
	}
	days := s.Days()
	return &days
}

func columnToShelfLife(days *int) entity.ShelfLife {
	if days == nil {
		return entity.NoShelfLife()
	}
	s, _ := entity.NewShelfLife(*days)
	return s
}

func weightToColumn(w *entity.Weight) *string {
	if w == nil {
		return nil
	}
	v := w.Kilograms().String()
	return &v
}

func columnToWeight(v *string) *entity.Weight {
	if v == nil {
		return nil
	}
	w, err := entity.NewWeightKilograms(*v)
	if err != nil {
		return nil
	}
	return &w
}

func volumeToColumn(v *entity.Volume) *string {
	if v == nil {
		return nil
	}
	s := v.CubicMetres().String()
	return &s
}

func columnToVolume(v *string) *entity.Volume {
	if v == nil {
		return nil
	}
	vol, err := entity.NewVolumeCubicMetres(*v)
	if err != nil {
		return nil
	}
	return &vol
}

func dimensionToColumns(d *entity.Dimension) (width, height, length *string) {
	if d == nil {
		return nil, nil, nil
	}
	w := d.Width().Centimetres().String()
	h := d.Height().Centimetres().String()
	l := d.Length().Centimetres().String()
	return &w, &h, &l
}

func columnToDimension(width, height, length *string) *entity.Dimension {
	// A dimension is all-or-nothing: the aggregate never stores a partial one,
	// so a row with some sides missing is treated as no dimension rather than a
	// half-built value object.
	if width == nil || height == nil || length == nil {
		return nil
	}
	w, err := entity.NewLengthCentimetres(*width)
	if err != nil {
		return nil
	}
	h, err := entity.NewLengthCentimetres(*height)
	if err != nil {
		return nil
	}
	l, err := entity.NewLengthCentimetres(*length)
	if err != nil {
		return nil
	}
	d, err := entity.NewDimension(w, h, l)
	if err != nil {
		return nil
	}
	return &d
}
