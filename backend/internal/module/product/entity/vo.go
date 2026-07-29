package entity

import (
	"strings"

	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type SKU string

func NewSKU(s string) (SKU, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return "", apperror.Validation("SKU required")
	}
	return SKU(s), nil
}

type ProductName string

func NewProductName(s string) (ProductName, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", apperror.Validation("Product name required")
	}
	return ProductName(s), nil
}

type Dimensions struct {
	Length, Width, Height float64
	Unit                  string
}
type Weight struct {
	Value float64
	Unit  string
}
type Volume struct {
	Value float64
	Unit  string
}

type TrackingConfig struct {
	LotTracking, SerialTracking, ExpiryTracking bool
	ShelfLifeDays                               int
}
type InventoryPolicy struct{ ReorderPoint, ReorderQty float64 }
