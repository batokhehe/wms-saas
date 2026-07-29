package dto

import (
	"time"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/google/uuid"
)

type ProductResponse struct {
	ID          uuid.UUID `json:"id"`
	SKU         string    `json:"sku"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateProductRequest struct {
	SKU             string                 `json:"sku" binding:"required"`
	Name            string                 `json:"name" binding:"required"`
	Description     string                 `json:"description"`
	CategoryID      uuid.UUID              `json:"category_id" binding:"required"`
	BrandID         uuid.UUID              `json:"brand_id" binding:"required"`
	BaseUOMID       uuid.UUID              `json:"base_uom_id" binding:"required"`
	TrackingConfig  TrackingConfigRequest  `json:"tracking_config" binding:"required"`
	InventoryPolicy InventoryPolicyRequest `json:"inventory_policy" binding:"required"`
}

type UpdateProductRequest struct {
	Name            string                 `json:"name" binding:"required"`
	Description     string                 `json:"description"`
	InventoryPolicy InventoryPolicyRequest `json:"inventory_policy" binding:"required"`
}

type TrackingConfigRequest struct {
	LotTracking    bool `json:"lot_tracking"`
	SerialTracking bool `json:"serial_tracking"`
	ExpiryTracking bool `json:"expiry_tracking"`
	ShelfLifeDays  int  `json:"shelf_life_days"`
}

type InventoryPolicyRequest struct {
	ReorderPoint float64 `json:"reorder_point"`
	ReorderQty   float64 `json:"reorder_qty"`
}

type IDParam struct {
	ID uuid.UUID `uri:"id" binding:"required,uuid"`
}

func (p IDParam) UUID() (uuid.UUID, error) { return p.ID, nil }

type ListProductQuery struct {
	pagination.Request
	Status string `form:"status" binding:"omitempty,oneof=ACTIVE INACTIVE"`
}

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
		},
	}
}
