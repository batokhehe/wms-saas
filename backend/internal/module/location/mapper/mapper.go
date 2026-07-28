// Package mapper converts the StorageLocation aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else.
//
// Note the direction: aggregate → DTO only. There is no FromCreateRequest,
// because building an aggregate is the FACTORY's job — entity.NewStorageLocation
// — and a mapper that constructed one would bypass the invariants the factory
// enforces.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/location/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an aggregate into its API representation.
//
// Everything is read through getters, which is the only access anyone has — the
// mapper is subject to exactly the same encapsulation as every other caller.
func ToResponse(l *entity.StorageLocation) dto.LocationResponse {
	if l == nil {
		return dto.LocationResponse{}
	}

	coordinate := l.Coordinate()
	capacity := l.Capacity()

	return dto.LocationResponse{
		ID:          l.ID(),
		WarehouseID: l.WarehouseID(),
		Code:        l.Code().String(),
		Coordinate: dto.CoordinateResponse{
			Zone:  coordinate.Zone(),
			Aisle: coordinate.Aisle(),
			Rack:  coordinate.Rack(),
			Level: coordinate.Level(),
			Bin:   coordinate.Bin(),
			Depth: coordinate.Depth(),
		},
		Barcode: l.Barcode().String(),
		Status:  l.Status().String(),

		PickingPriority: l.PickingPriority(),
		AllowMixedSKU:   l.AllowMixedSKU(),
		AllowOverflow:   l.AllowOverflow(),
		Capacity: dto.CapacityResponse{
			MaxWeight:   capacity.MaxWeight().String(),
			MaxVolume:   capacity.MaxVolume().String(),
			MaxPallet:   capacity.MaxPallet(),
			IsUnlimited: capacity.IsUnlimited(),
		},

		// Asked of the aggregate rather than derived from the status. The two
		// genuinely differ — a MAINTENANCE location may be picked from but not
		// received into — so a client computing them from `status` would get it
		// wrong.
		CanReceive: l.CanReceiveInventory(),
		CanPick:    l.CanPickInventory(),

		IsArchived: l.IsArchived(),
		ArchivedAt: l.DeletedAt(),

		CreatedBy: l.CreatedBy(),
		UpdatedBy: l.UpdatedBy(),
		CreatedAt: l.CreatedAt(),
		UpdatedAt: l.UpdatedAt(),
	}
}

// ToPage converts a page of aggregates, preserving the pagination metadata.
func ToPage(
	page pagination.Page[*entity.StorageLocation],
) pagination.Page[dto.LocationResponse] {
	return pagination.MapPage(page, func(l *entity.StorageLocation) dto.LocationResponse {
		return ToResponse(l)
	})
}
