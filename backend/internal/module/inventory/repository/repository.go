// Package repository is the inventory module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository. It translates between the InventoryPosition
// aggregate — whose fields are unexported — and a persistence model GORM can map.
package repository

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ListFilter narrows a position listing. A plain struct rather than a DTO: the
// repository must not depend on the transport contract.
type ListFilter struct {
	Paging      pagination.Request
	WarehouseID uuid.UUID
	LocationID  uuid.UUID
	ProductID   uuid.UUID
	Tracking    string
}

// Repository is the persistence contract for the InventoryPosition aggregate.
//
// Every method takes a tenant — either directly or inside a StockKey — because
// positions are tenant-owned and forgetting the filter must not compile.
type Repository interface {
	// GetOrCreatePosition returns the position a StockKey addresses, opening an
	// EMPTY one when it does not exist yet.
	//
	// It is the aggregate's single entry point for stock that may or may not have
	// a position already: a first receipt and a hundredth receipt call the same
	// method. The created position is NOT persisted here — the caller books its
	// movement through the aggregate first and then Updates, so an empty position
	// is never written for an operation that fails.
	GetOrCreatePosition(ctx context.Context, key entity.StockKey, actorID uuid.UUID) (*entity.InventoryPosition, error)

	// Update persists a position under optimistic locking, inserting it when it
	// is new. A stale version yields ErrConcurrentModification.
	Update(ctx context.Context, position *entity.InventoryPosition) error

	// FindByID returns one position, or NOT_FOUND. A position belonging to
	// another company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it
	// exists.
	FindByID(ctx context.Context, positionID, companyID uuid.UUID) (*entity.InventoryPosition, error)

	// FindByKey resolves the position a StockKey addresses, or NOT_FOUND.
	FindByKey(ctx context.Context, key entity.StockKey) (*entity.InventoryPosition, error)

	// List returns a page of the company's positions.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.InventoryPosition], error)
}
