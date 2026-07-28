// Package mapper converts the Product aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else.
//
// Note the direction: aggregate → DTO only. There is no FromCreateRequest here,
// because building an aggregate is the FACTORY's job — entity.NewProduct — and a
// mapper that constructed one would bypass the invariants the factory enforces.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation.
//
// Everything is read through getters, which is the only access anyone has — the
// mapper is subject to exactly the same encapsulation as every other caller.
func ToResponse(p *entity.Product) dto.ProductResponse {
	if p == nil {
		return dto.ProductResponse{}
	}

	resp := dto.ProductResponse{
		ID:          p.ID(),
		SKU:         p.SKU().String(),
		Name:        p.Name().String(),
		Description: p.Description(),
		CategoryID:  p.CategoryID(),
		BrandID:     p.BrandID(),
		BaseUOMID:   p.BaseUOMID(),
		Status:      p.Status().String(),
		Tracking:    p.TrackingMethod().String(),
		Barcodes:    toBarcodes(p),
		UOMs:        toUOMs(p),
		CreatedBy:   p.CreatedBy(),
		UpdatedBy:   p.UpdatedBy(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}

	// Shelf life: a defined zero-day value must be reported as defined with days
	// 0, distinct from an undefined shelf life which omits the day count.
	if shelfLife := p.ShelfLife(); shelfLife.IsDefined() {
		resp.ShelfLifeDefined = true
		days := shelfLife.Days()
		resp.ShelfLifeDays = &days
	}

	if weight := p.Weight(); weight != nil {
		v := weight.Kilograms().String()
		resp.Weight = &v
	}
	if volume := p.Volume(); volume != nil {
		v := volume.CubicMetres().String()
		resp.Volume = &v
	}
	if dimension := p.Dimension(); dimension != nil {
		resp.Dimension = &dto.DimensionResponse{
			Width:  dimension.Width().Centimetres().String(),
			Height: dimension.Height().Centimetres().String(),
			Length: dimension.Length().Centimetres().String(),
		}
	}

	return resp
}

// toBarcodes renders the barcode collection. A non-nil empty slice, so the JSON
// is [] rather than null — a client forced to handle both will crash on one.
func toBarcodes(p *entity.Product) []dto.BarcodeResponse {
	barcodes := p.Barcodes()
	out := make([]dto.BarcodeResponse, 0, len(barcodes))
	for _, b := range barcodes {
		out = append(out, dto.BarcodeResponse{
			Barcode:   b.Barcode().String(),
			IsPrimary: b.IsPrimary(),
		})
	}
	return out
}

// toUOMs renders the alternate-unit collection, marking the base unit.
func toUOMs(p *entity.Product) []dto.UOMResponse {
	uoms := p.UOMs()
	base := p.BaseUOMID()
	out := make([]dto.UOMResponse, 0, len(uoms))
	for _, u := range uoms {
		out = append(out, dto.UOMResponse{
			UOMID:  u.UOMID(),
			Factor: u.ConversionFactor().Decimal().String(),
			IsBase: u.UOMID() == base,
		})
	}
	return out
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.Product]) pagination.Page[dto.ProductResponse] {
	return pagination.MapPage(page, ToResponse)
}
