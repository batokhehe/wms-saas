// Package mapper converts the InventoryLedgerEntry aggregate into transport DTOs.
//
// LAYER RULE: conversion lives here and nowhere else, and the direction is
// aggregate → DTO only. There is no FromRequest: building an entry is the
// factory's job, and a mapper that constructed one would bypass the validation
// that keeps a delta consistent with its snapshots.
package mapper

import (
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ToResponse converts an entry into its API representation, reading through
// getters — the mapper is subject to the same encapsulation as every caller.
//
// OnHand and the delta are asked of the AGGREGATE rather than recomputed here:
// both are domain derivations, and a second implementation in the transport layer
// would be free to drift from the first.
func ToResponse(e *entity.InventoryLedgerEntry) dto.LedgerEntryResponse {
	if e == nil {
		return dto.LedgerEntryResponse{}
	}
	before, after, delta := e.Before(), e.After(), e.Delta()

	return dto.LedgerEntryResponse{
		ID:        e.LedgerID(),
		CompanyID: e.CompanyID(),

		PositionID:  e.PositionID(),
		ProductID:   e.ProductID(),
		WarehouseID: e.WarehouseID(),
		LocationID:  e.LocationID(),

		LotNumber:    e.LotNumber(),
		SerialNumber: e.SerialNumber(),
		OwnerID:      e.OwnerID(),

		MovementType: e.MovementType().String(),

		ReferenceType:  e.ReferenceType(),
		ReferenceID:    e.ReferenceID(),
		DocumentNumber: e.DocumentNumber(),
		Reason:         e.Reason(),

		ActorID: e.ActorID(),

		Before: dto.BucketSnapshotResponse{
			Available:   before.Available(),
			Reserved:    before.Reserved(),
			Allocated:   before.Allocated(),
			Quarantined: before.Quarantined(),
			OnHand:      before.OnHand(),
		},
		After: dto.BucketSnapshotResponse{
			Available:   after.Available(),
			Reserved:    after.Reserved(),
			Allocated:   after.Allocated(),
			Quarantined: after.Quarantined(),
			OnHand:      after.OnHand(),
		},
		Delta: dto.BucketDeltaResponse{
			Available:   delta.Available(),
			Reserved:    delta.Reserved(),
			Allocated:   delta.Allocated(),
			Quarantined: delta.Quarantined(),
			OnHand:      delta.OnHand(),
		},

		OccurredAt: e.OccurredAt(),
	}
}

// ToPage converts a page of entries, preserving the pagination metadata.
func ToPage(page pagination.Page[*entity.InventoryLedgerEntry]) pagination.Page[dto.LedgerEntryResponse] {
	return pagination.MapPage(page, ToResponse)
}
