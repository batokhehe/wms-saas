// Package mapper converts the Warehouse aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else.
//
// Note the direction: aggregate → DTO only. There is no FromCreateRequest here,
// because building an aggregate is the FACTORY's job — entity.NewWarehouse —
// and a mapper that constructed one would bypass the invariants the factory
// enforces. The other modules' mappers have that function because their
// entities are plain structs; this one's is not.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation.
//
// Everything is read through getters, which is the only access anyone has —
// the mapper is subject to exactly the same encapsulation as every other
// caller.
func ToResponse(w *entity.Warehouse) dto.WarehouseResponse {
	if w == nil {
		return dto.WarehouseResponse{}
	}

	zones := w.Zones()

	return dto.WarehouseResponse{
		ID:           w.ID(),
		Code:         w.Code().String(),
		Name:         w.Name(),
		Description:  w.Description(),
		Type:         w.Type().String(),
		Status:       w.Status().String(),
		Address:      w.Address().String(),
		ContactName:  w.Contact().Name(),
		ContactPhone: w.Contact().Phone(),
		Zones: dto.ZonesResponse{
			ReceivingZoneID: zones.Receiving(),
			ShippingZoneID:  zones.Shipping(),
			StagingZoneID:   zones.Staging(),
		},

		// Asked of the aggregate rather than derived from the status. When
		// "receiving requires a receiving zone" becomes true, it changes in one
		// place and every client inherits it.
		CanReceive: w.CanReceiveInventory(),
		CanShip:    w.CanShipInventory(),

		IsArchived: w.IsArchived(),
		ArchivedAt: w.DeletedAt(),

		CreatedBy: w.CreatedBy(),
		UpdatedBy: w.UpdatedBy(),
		CreatedAt: w.CreatedAt(),
		UpdatedAt: w.UpdatedAt(),
	}
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(
	page pagination.Page[*entity.Warehouse],
) pagination.Page[dto.WarehouseResponse] {
	return pagination.MapPage(page, func(w *entity.Warehouse) dto.WarehouseResponse {
		return ToResponse(w)
	})
}
