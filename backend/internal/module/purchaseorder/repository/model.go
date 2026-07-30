// Package repository is the purchase-order module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. It translates between the aggregate (unexported
// fields) and persistence models GORM can map, exactly as the product, supplier
// and inventory repositories do.
package repository

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
)

// purchaseOrderModel is the persistence representation of the aggregate root.
//
// entity.PurchaseOrder has unexported fields GORM cannot reflect over; exporting
// them would delete the encapsulation the aggregate rests on. So this model
// absorbs the ORM.
//
// Lines are NOT declared as a GORM association here. They are loaded and written
// explicitly by the repository, because "replace the whole line set" is a
// delete-then-insert that GORM's association handling does not express, and
// because an implicit association silently returns an empty slice when a caller
// forgets to Preload — which would look exactly like an order with no lines.
type purchaseOrderModel struct {
	sharedentity.BaseEntity

	CompanyID uuid.UUID `gorm:"type:uuid;not null;index"`

	Number string `gorm:"type:citext;not null"`

	SupplierID  uuid.UUID `gorm:"column:supplier_id;type:uuid;not null;index"`
	WarehouseID uuid.UUID `gorm:"column:warehouse_id;type:uuid;not null;index"`

	OrderDate           time.Time `gorm:"column:order_date;not null"`
	ExpectedArrivalDate time.Time `gorm:"column:expected_arrival_date;not null"`

	Status  string `gorm:"type:varchar(24);not null;default:DRAFT"`
	Remarks string `gorm:"type:text;not null;default:''"`

	CreatedBy  uuid.UUID  `gorm:"column:created_by;type:uuid;not null"`
	ApprovedBy *uuid.UUID `gorm:"column:approved_by;type:uuid"`
	ApprovedAt *time.Time `gorm:"column:approved_at"`
	UpdatedBy  uuid.UUID  `gorm:"column:updated_by;type:uuid;not null"`
}

// TableName pins the table name so a struct rename cannot silently change the
// schema GORM targets.
func (purchaseOrderModel) TableName() string { return "purchase_orders" }

// purchaseOrderLineModel is the persistence representation of a child line.
//
// RemainingQty has NO column: it is derived by the aggregate from ordered minus
// received. Storing it would create a third number that can drift from the two
// it summarises.
type purchaseOrderLineModel struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	PurchaseOrderID uuid.UUID `gorm:"column:purchase_order_id;type:uuid;not null;index"`

	ProductID uuid.UUID `gorm:"column:product_id;type:uuid;not null;index"`
	UOMID     uuid.UUID `gorm:"column:uom_id;type:uuid;not null"`

	OrderedQty  int64 `gorm:"column:ordered_qty;not null"`
	ReceivedQty int64 `gorm:"column:received_qty;not null;default:0"`

	// UnitPrice is nullable so "not priced" stays distinct from "free".
	UnitPrice *int64 `gorm:"column:unit_price"`

	Remarks string `gorm:"type:text;not null;default:''"`

	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

// TableName pins the table name.
func (purchaseOrderLineModel) TableName() string { return "purchase_order_lines" }

// ---------------------------------------------------------------------------
// Translation
// ---------------------------------------------------------------------------

// toModel translates an aggregate into its persistence form, reading through the
// aggregate's getters — the only access anyone has.
func toModel(o *entity.PurchaseOrder) *purchaseOrderModel {
	model := &purchaseOrderModel{
		CompanyID:           o.CompanyID(),
		Number:              o.Number().String(),
		SupplierID:          o.SupplierID(),
		WarehouseID:         o.WarehouseID(),
		OrderDate:           o.OrderDate(),
		ExpectedArrivalDate: o.ExpectedArrivalDate(),
		Status:              o.Status().String(),
		Remarks:             o.Remarks(),
		CreatedBy:           o.CreatedBy(),
		ApprovedBy:          o.ApprovedBy(),
		ApprovedAt:          o.ApprovedAt(),
		UpdatedBy:           o.UpdatedBy(),
	}
	model.ID = o.ID()
	model.Version = o.Version()
	model.CreatedAt = o.CreatedAt()
	model.UpdatedAt = o.UpdatedAt()
	return model
}

// toLineModels translates the aggregate's line set into rows.
func toLineModels(o *entity.PurchaseOrder) []purchaseOrderLineModel {
	lines := o.Lines()
	rows := make([]purchaseOrderLineModel, 0, len(lines))
	for _, line := range lines {
		var price *int64
		if !line.UnitPrice().IsZero() {
			amount := line.UnitPrice().Amount()
			price = &amount
		}
		rows = append(rows, purchaseOrderLineModel{
			ID:              line.ID(),
			PurchaseOrderID: o.ID(),
			ProductID:       line.ProductID(),
			UOMID:           line.UOMID(),
			OrderedQty:      line.OrderedQty().Value(),
			ReceivedQty:     line.ReceivedQty().Value(),
			UnitPrice:       price,
			Remarks:         line.Remarks(),
			CreatedAt:       o.CreatedAt(),
			UpdatedAt:       o.UpdatedAt(),
		})
	}
	return rows
}

// toDomain rebuilds an aggregate from rows via entity.Reconstitute, NOT the
// factory: loading a row is not a business event.
//
// A row that cannot be reconstituted is CORRUPT — it violates an invariant the
// database was supposed to hold — so the error is returned rather than
// swallowed. Silently dropping it would hand the service an aggregate that
// disagrees with storage.
func toDomain(model *purchaseOrderModel, lineRows []purchaseOrderLineModel) (*entity.PurchaseOrder, error) {
	number, err := entity.NewOrderNumber(model.Number)
	if err != nil {
		return nil, err
	}
	status, err := entity.NewStatus(model.Status)
	if err != nil {
		return nil, err
	}

	lines := make([]entity.PurchaseOrderLine, 0, len(lineRows))
	for _, row := range lineRows {
		ordered, err := entity.NewQuantity(row.OrderedQty)
		if err != nil {
			return nil, err
		}
		received, err := entity.NewQuantity(row.ReceivedQty)
		if err != nil {
			return nil, err
		}
		price := entity.NoMoney()
		if row.UnitPrice != nil {
			price, err = entity.NewMoney(*row.UnitPrice)
			if err != nil {
				return nil, err
			}
		}
		line, err := entity.ReconstituteLine(
			row.ID, row.ProductID, row.UOMID, ordered, received, price, row.Remarks,
		)
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}

	return entity.Reconstitute(
		model.ID, model.CompanyID, number,
		model.SupplierID, model.WarehouseID,
		model.OrderDate, model.ExpectedArrivalDate,
		status, model.Remarks, lines,
		model.Version,
		model.CreatedBy, model.ApprovedBy, model.ApprovedAt,
		model.UpdatedBy, model.CreatedAt, model.UpdatedAt,
	)
}
