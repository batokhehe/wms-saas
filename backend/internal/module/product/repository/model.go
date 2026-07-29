package repository

import (
	sharedentity "github.com/batokhehe/wms-saas/backend/internal/shared/entity"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type productModel struct {
	sharedentity.BaseEntity
	CompanyID       uuid.UUID      `gorm:"type:uuid;not null;index"`
	CategoryID      uuid.UUID      `gorm:"type:uuid;not null;index"`
	BrandID         uuid.UUID      `gorm:"type:uuid;not null;index"`
	BaseUOMID       uuid.UUID      `gorm:"type:uuid;not null;index"`
	SKU             string         `gorm:"type:citext;not null"`
	Name            string         `gorm:"type:varchar(255);not null"`
	Description     string         `gorm:"type:text;not null;default:''"`
	TrackingConfig  datatypes.JSON `gorm:"type:jsonb;not null"`
	InventoryPolicy datatypes.JSON `gorm:"type:jsonb;not null"`
	Dimensions      datatypes.JSON `gorm:"type:jsonb;not null"`
	Weight          datatypes.JSON `gorm:"type:jsonb;not null"`
	Volume          datatypes.JSON `gorm:"type:jsonb;not null"`
	Status          string         `gorm:"type:varchar(16);not null;default:ACTIVE"`
	Barcodes        []barcodeModel `gorm:"foreignKey:ProductID"`
	AlternateUOMs   []uomModel     `gorm:"foreignKey:ProductID"`
}

type barcodeModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProductID uuid.UUID `gorm:"type:uuid;not null;index"`
	Code      string    `gorm:"type:citext;not null"`
	Type      string    `gorm:"type:varchar(50);not null"`
	IsPrimary bool      `gorm:"not null"`
}

type uomModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProductID uuid.UUID `gorm:"type:uuid;not null;index"`
	UOMID     uuid.UUID `gorm:"type:uuid;not null;index"`
	Factor    float64   `gorm:"type:decimal(19,6);not null"`
}

func (productModel) TableName() string { return "products" }
func (barcodeModel) TableName() string { return "product_barcodes" }
func (uomModel) TableName() string     { return "product_alternate_uoms" }
