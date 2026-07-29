// Package mapper converts the InventoryPosition aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. The direction is aggregate
// → DTO only; building a position is the factory's job.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts a position into its API representation, reading through
// getters — the mapper is subject to the same encapsulation as every caller.
//
// OnHand is asked of the AGGREGATE rather than summed here: the derivation is a
// domain rule, and recomputing it in the transport layer would be a second
// implementation free to drift from the first.
func ToResponse(p *entity.InventoryPosition) dto.PositionResponse {
	if p == nil {
		return dto.PositionResponse{}
	}
	attrs := p.Attributes()

	resp := dto.PositionResponse{
		ID:          p.ID(),
		CompanyID:   p.CompanyID(),
		WarehouseID: p.WarehouseID(),
		LocationID:  p.LocationID(),
		ProductID:   p.ProductID(),
		Tracking:    attrs.Tracking().String(),
		Available:   p.Available().Value(),
		Reserved:    p.Reserved().Value(),
		Allocated:   p.Allocated().Value(),
		Quarantined: p.Quarantined().Value(),
		OnHand:      p.OnHand(),
		CreatedBy:   p.CreatedBy(),
		UpdatedBy:   p.UpdatedBy(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}

	if attrs.HasLot() {
		lot := attrs.Lot().String()
		resp.LotNumber = &lot
	}
	if attrs.HasSerial() {
		serial := attrs.Serial().String()
		resp.SerialNumber = &serial
	}

	return resp
}

// ToPage converts a page of positions, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.InventoryPosition]) pagination.Page[dto.PositionResponse] {
	return pagination.MapPage(page, ToResponse)
}
