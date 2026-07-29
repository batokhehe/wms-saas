// Package repository is the inventory-ledger module's persistence layer.
//
// LAYER RULE: the only package in this module permitted to import gorm or
// internal/shared/repository.
//
// The contract below is deliberately INCOMPLETE by CRUD standards: there is an
// Append and there are reads, and there is no Update and no Delete. That is not
// an omission to be filled in later — a ledger that can be edited is not a
// ledger. The absence of those methods is the module's central guarantee,
// reinforced by a database trigger that rejects UPDATE and DELETE outright.
package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
)

// ListFilter narrows a ledger query. A plain struct rather than a DTO: the
// repository must not depend on the transport contract.
//
// Every field is optional; a zero value means "do not filter on this". The date
// range is half-open — From inclusive, To exclusive — so consecutive periods
// tile without double-counting an entry that lands exactly on a boundary.
type ListFilter struct {
	Paging pagination.Request

	PositionID   uuid.UUID
	ProductID    uuid.UUID
	WarehouseID  uuid.UUID
	LocationID   uuid.UUID
	MovementType string

	ReferenceType string
	ReferenceID   uuid.UUID

	OccurredFrom *time.Time
	OccurredTo   *time.Time
}

// Repository is the persistence contract for the inventory ledger.
//
// Every method takes a companyID: entries are tenant-owned, and making the tenant
// a required argument means forgetting it does not compile.
type Repository interface {
	// Append writes a new entry. It is the ONLY write operation the ledger has.
	//
	// Appending the same entry id twice is a CONFLICT rather than an overwrite,
	// which is what makes a retried publish safe: the second attempt is refused
	// instead of silently rewriting history.
	Append(ctx context.Context, entry *entity.InventoryLedgerEntry) error

	// FindByID returns one entry, or NOT_FOUND. An entry belonging to another
	// company is NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
	FindByID(ctx context.Context, ledgerID, companyID uuid.UUID) (*entity.InventoryLedgerEntry, error)

	// FindByPosition returns the movement history of one position, newest first.
	FindByPosition(ctx context.Context, companyID, positionID uuid.UUID, paging pagination.Request) (pagination.Page[*entity.InventoryLedgerEntry], error)

	// FindByReference returns every entry caused by one business document —
	// the audit question "what did this receipt actually move?".
	FindByReference(ctx context.Context, companyID uuid.UUID, referenceType string, referenceID uuid.UUID) ([]*entity.InventoryLedgerEntry, error)

	// List returns a filtered, paginated page of the company's entries.
	List(ctx context.Context, companyID uuid.UUID, filter ListFilter) (pagination.Page[*entity.InventoryLedgerEntry], error)
}
