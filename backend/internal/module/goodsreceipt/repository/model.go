package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// goodsReceiptModel is the persistence representation of the aggregate root.
//
// entity.GoodsReceipt has unexported fields GORM cannot reflect over; exporting
// them would delete the encapsulation the aggregate rests on. So this model
// absorbs the ORM.
//
// Lines are NOT declared as a GORM association: they are loaded and written
// explicitly, because "replace the whole line set" is a delete-then-insert that
// GORM's association handling does not express, and an implicit association
// silently returns an empty slice when a caller forgets to Preload — which looks
// exactly like a receipt with no lines.
type goodsReceiptModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	Number string `gorm:"type:varchar(32);not null"`

	WarehouseID uuid.UUID  `gorm:"column:warehouse_id;type:uuid;not null;index"`
	SupplierID  *uuid.UUID `gorm:"column:supplier_id;type:uuid"`

	ReferenceType string     `gorm:"column:reference_type;type:varchar(32);not null"`
	ReferenceID   *uuid.UUID `gorm:"column:reference_id;type:uuid"`

	ReceiptDate time.Time `gorm:"column:receipt_date;not null"`

	Status  string `gorm:"type:varchar(16);not null;default:DRAFT"`
	Remarks string `gorm:"type:text;not null;default:''"`

	CreatedBy  uuid.UUID  `gorm:"column:created_by;type:uuid;not null"`
	ReceivedBy *uuid.UUID `gorm:"column:received_by;type:uuid"`
	UpdatedBy  uuid.UUID  `gorm:"column:updated_by;type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (goodsReceiptModel) TableName() string { return "goods_receipts" }

// goodsReceiptLineModel is the persistence representation of a child line.
type goodsReceiptLineModel struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	GoodsReceiptID uuid.UUID `gorm:"column:goods_receipt_id;type:uuid;not null;index"`

	ProductID  uuid.UUID `gorm:"column:product_id;type:uuid;not null;index"`
	LocationID uuid.UUID `gorm:"column:location_id;type:uuid;not null"`
	UOMID      uuid.UUID `gorm:"column:uom_id;type:uuid;not null"`

	Quantity int64 `gorm:"not null"`

	BatchNumber *string `gorm:"column:batch_number;type:varchar(64)"`
	LotNumber   *string `gorm:"column:lot_number;type:varchar(64)"`

	// pq.StringArray maps PostgreSQL's TEXT[] — the only column in this module
	// that needs a driver-specific type.
	SerialNumbers pq.StringArray `gorm:"column:serial_numbers;type:text[]"`

	ExpiryDate *time.Time `gorm:"column:expiry_date"`
	Remarks    string     `gorm:"type:text;not null;default:''"`

	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

// TableName pins the table name.
func (goodsReceiptLineModel) TableName() string { return "goods_receipt_lines" }

// ---------------------------------------------------------------------------
// Translation
// ---------------------------------------------------------------------------

// toModel translates an aggregate into its persistence form, reading through the
// aggregate's getters — the only access anyone has.
func toModel(g *entity.GoodsReceipt) *goodsReceiptModel {
	model := &goodsReceiptModel{
		CompanyID:     g.CompanyID(),
		Number:        g.Number().String(),
		WarehouseID:   g.WarehouseID(),
		SupplierID:    g.SupplierID(),
		ReferenceType: g.Reference().Kind().String(),
		ReferenceID:   g.Reference().ID(),
		ReceiptDate:   g.ReceiptDate(),
		Status:        g.Status().String(),
		Remarks:       g.Remarks(),
		CreatedBy:     g.CreatedBy(),
		ReceivedBy:    g.ReceivedBy(),
		UpdatedBy:     g.UpdatedBy(),
	}
	model.ID = g.ID()
	model.Version = g.Version()
	model.CreatedAt = g.CreatedAt()
	model.UpdatedAt = g.UpdatedAt()
	return model
}

// toLineModels translates the aggregate's line set into rows.
func toLineModels(g *entity.GoodsReceipt) []goodsReceiptLineModel {
	lines := g.Lines()
	rows := make([]goodsReceiptLineModel, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, goodsReceiptLineModel{
			ID:             line.ID(),
			GoodsReceiptID: g.ID(),
			ProductID:      line.ProductID(),
			LocationID:     line.LocationID(),
			UOMID:          line.UOMID(),
			Quantity:       line.Quantity().Value(),
			BatchNumber:    optional(line.BatchNumber()),
			LotNumber:      optional(line.LotNumber()),
			SerialNumbers:  pq.StringArray(line.SerialNumbers()),
			ExpiryDate:     line.ExpiryDate(),
			Remarks:        line.Remarks(),
			CreatedAt:      g.CreatedAt(),
			UpdatedAt:      g.UpdatedAt(),
		})
	}
	return rows
}

// toDomain rebuilds an aggregate from rows via entity.Reconstitute, NOT the
// factory: loading a row is not a business event.
//
// A row that cannot be reconstituted is CORRUPT — it violates an invariant the
// database was supposed to hold — so the error is returned rather than
// swallowed.
func toDomain(model *goodsReceiptModel, lineRows []goodsReceiptLineModel) (*entity.GoodsReceipt, error) {
	number, err := entity.NewReceiptNumber(model.Number)
	if err != nil {
		return nil, err
	}
	status, err := entity.NewStatus(model.Status)
	if err != nil {
		return nil, err
	}
	reference, err := entity.NewDocumentReference(
		entity.ReferenceType(model.ReferenceType), model.ReferenceID,
	)
	if err != nil {
		return nil, err
	}

	lines := make([]entity.GoodsReceiptLine, 0, len(lineRows))
	for _, row := range lineRows {
		quantity, err := entity.NewQuantity(row.Quantity)
		if err != nil {
			return nil, err
		}
		line, err := entity.ReconstituteLine(
			row.ID, row.ProductID, row.LocationID, row.UOMID, quantity,
			value(row.BatchNumber), value(row.LotNumber),
			[]string(row.SerialNumbers), row.ExpiryDate, row.Remarks,
		)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}

	return entity.Reconstitute(
		model.ID, model.CompanyID, number,
		model.WarehouseID, model.SupplierID, reference,
		model.ReceiptDate, status, model.Remarks, lines,
		model.Version,
		model.CreatedBy, model.ReceivedBy, model.UpdatedBy,
		model.CreatedAt, model.UpdatedAt,
	)
}

// optional renders an empty string as SQL NULL, so "unset" stays distinct from
// "empty".
func optional(raw string) *string {
	if raw == "" {
		return nil
	}
	return &raw
}

func value(raw *string) string {
	if raw == nil {
		return ""
	}
	return *raw
}
