// Package mapper converts the Inventory aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else. The direction is aggregate
// → DTO only. There is no FromCreateRequest, because building an aggregate is the
// FACTORY's job — entity.NewInventory — and a mapper that constructed one would
// bypass the invariants the factory enforces.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation. Everything is
// read through getters — the mapper is subject to exactly the same encapsulation
// as every other caller.
func ToResponse(inv *entity.Inventory) dto.InventoryResponse {
	if inv == nil {
		return dto.InventoryResponse{}
	}

	resp := dto.InventoryResponse{
		ID:          inv.ID(),
		CompanyID:   inv.CompanyID(),
		WarehouseID: inv.WarehouseID(),
		LocationID:  inv.LocationID(),
		ProductID:   inv.ProductID(),
		Tracking:    inv.TrackingType().String(),
		Status:      inv.Status().String(),
		OnHand:      inv.OnHand().Value(),
		Reserved:    inv.Reserved().Value(),
		Available:   inv.Available().Value(),
		CreatedBy:   inv.CreatedBy(),
		UpdatedBy:   inv.UpdatedBy(),
		CreatedAt:   inv.CreatedAt(),
		UpdatedAt:   inv.UpdatedAt(),
	}

	if inv.HasLot() {
		lot := inv.Lot().String()
		resp.LotNumber = &lot
	}
	if inv.HasSerial() {
		serial := inv.Serial().String()
		resp.SerialNumber = &serial
	}

	return resp
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.Inventory]) pagination.Page[dto.InventoryResponse] {
	return pagination.MapPage(page, ToResponse)
}
