// Package dto holds the inventory-ledger module's transport contracts.
//
// LAYER RULE: DTOs are never entities. The ledger's HTTP surface is READ-ONLY, so
// there is no create/update request here — RecordMovementRequest exists for the
// in-process caller (the Inventory module), not for a client.
package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ---------- Inbound (in-process only) ----------

// BucketSnapshotRequest is a four-balance snapshot of a position.
type BucketSnapshotRequest struct {
	Available   int64 `json:"available"`
	Reserved    int64 `json:"reserved"`
	Allocated   int64 `json:"allocated"`
	Quarantined int64 `json:"quarantined"`
}

// RecordMovementRequest asks the ledger to witness a transition.
//
// It carries no delta: the service computes it from Before and After, so a caller
// cannot record a change that contradicts the balances it reports.
//
// This type is NOT bound from an HTTP body anywhere — the ledger exposes no write
// endpoint. It is the in-process contract the Inventory module calls through.
type RecordMovementRequest struct {
	PositionID  uuid.UUID
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	LocationID  uuid.UUID

	LotNumber    string
	SerialNumber string
	OwnerID      *uuid.UUID

	MovementType string

	ReferenceType  string
	ReferenceID    *uuid.UUID
	DocumentNumber string
	Reason         string

	Before BucketSnapshotRequest
	After  BucketSnapshotRequest

	// OccurredAt is BUSINESS time. Zero means "now", which the service resolves
	// from the injected clock rather than calling time.Now itself.
	OccurredAt time.Time
}

// ---------- Queries ----------

// ListLedgerQuery is the ledger listing query string.
//
// The id filters bind as strings and are parsed after validation, because Gin's
// form binder rejects uuid.UUID (a [16]byte array). The dates bind as RFC3339.
type ListLedgerQuery struct {
	pagination.Request

	PositionID  string `form:"position_id"  binding:"omitempty,uuid"`
	ProductID   string `form:"product_id"   binding:"omitempty,uuid"`
	WarehouseID string `form:"warehouse_id" binding:"omitempty,uuid"`
	LocationID  string `form:"location_id"  binding:"omitempty,uuid"`

	MovementType string `form:"movement_type" binding:"omitempty,oneof=INITIAL_BALANCE INBOUND OUTBOUND TRANSFER RESERVATION ALLOCATION ADJUSTMENT QUARANTINE CYCLE_COUNT"`

	ReferenceType string `form:"reference_type" binding:"omitempty,max=64"`
	ReferenceID   string `form:"reference_id"   binding:"omitempty,uuid"`

	// Half-open range: from inclusive, to exclusive, so consecutive periods tile
	// without double-counting a boundary entry.
	OccurredFrom string `form:"occurred_from" binding:"omitempty"`
	OccurredTo   string `form:"occurred_to"   binding:"omitempty"`
}

// ParseOccurredFrom parses the inclusive lower bound, or nil when absent.
func (q ListLedgerQuery) ParseOccurredFrom() (*time.Time, error) {
	return parseTime("occurred_from", q.OccurredFrom)
}

// ParseOccurredTo parses the exclusive upper bound, or nil when absent.
func (q ListLedgerQuery) ParseOccurredTo() (*time.Time, error) {
	return parseTime("occurred_to", q.OccurredTo)
}

// parseTime accepts RFC3339, or a bare date which is read as midnight UTC so a
// caller can write ?occurred_from=2026-08-01 without a clock component.
func parseTime(field, raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return &utc, nil
		}
	}
	return nil, apperror.NewValidation(apperror.FieldError{
		Field:   field,
		Rule:    "datetime",
		Message: "must be an RFC3339 timestamp or a YYYY-MM-DD date",
	}).WithOp("inventoryledger.dto.parseTime")
}

// SortOptions declares this endpoint's paging rules.
//
// AllowedSorts is a security control: ORDER BY cannot be parameterised, so only
// keys listed here reach the SQL. The default is occurred_at DESC because a
// ledger is read newest-first.
func SortOptions() pagination.Options {
	return pagination.Options{
		DefaultLimit: 50,
		MaxLimit:     200,
		DefaultSort:  "occurred_at",
		DefaultOrder: pagination.OrderDesc,
		AllowedSorts: map[string]string{
			"occurred_at":   "inventory_ledger_entries.occurred_at",
			"created_at":    "inventory_ledger_entries.created_at",
			"movement_type": "inventory_ledger_entries.movement_type",
		},
	}
}

// IDParam binds a UUID path parameter as a string, then parses it.
type IDParam struct {
	ID string `uri:"id" binding:"required,uuid"`
}

// UUID returns the parsed identifier.
func (p IDParam) UUID() (uuid.UUID, error) {
	return parseParam("id", p.ID)
}

// PositionParam binds the :positionId path parameter.
type PositionParam struct {
	PositionID string `uri:"positionId" binding:"required,uuid"`
}

// UUID returns the parsed position identifier.
func (p PositionParam) UUID() (uuid.UUID, error) {
	return parseParam("positionId", p.PositionID)
}

func parseParam(field, raw string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewValidation(apperror.FieldError{
			Field:   field,
			Rule:    "uuid",
			Message: field + " must be a valid UUID",
		}).WithOp("inventoryledger.dto.parseParam").WithCause(err)
	}
	return parsed, nil
}

// ---------- Responses ----------

// BucketSnapshotResponse reports a four-balance snapshot plus its derived total.
type BucketSnapshotResponse struct {
	Available   int64 `json:"available"`
	Reserved    int64 `json:"reserved"`
	Allocated   int64 `json:"allocated"`
	Quarantined int64 `json:"quarantined"`
	OnHand      int64 `json:"on_hand"`
}

// BucketDeltaResponse reports the signed change each bucket underwent.
type BucketDeltaResponse struct {
	Available   int64 `json:"available"`
	Reserved    int64 `json:"reserved"`
	Allocated   int64 `json:"allocated"`
	Quarantined int64 `json:"quarantined"`
	OnHand      int64 `json:"on_hand"`
}

// LedgerEntryResponse is the public representation of one ledger entry.
type LedgerEntryResponse struct {
	ID        uuid.UUID `json:"id"`
	CompanyID uuid.UUID `json:"company_id"`

	PositionID  uuid.UUID `json:"position_id"`
	ProductID   uuid.UUID `json:"product_id"`
	WarehouseID uuid.UUID `json:"warehouse_id"`
	LocationID  uuid.UUID `json:"location_id"`

	LotNumber    string     `json:"lot_number,omitempty"`
	SerialNumber string     `json:"serial_number,omitempty"`
	OwnerID      *uuid.UUID `json:"owner_id,omitempty"`

	MovementType string `json:"movement_type"`

	ReferenceType  string     `json:"reference_type,omitempty"`
	ReferenceID    *uuid.UUID `json:"reference_id,omitempty"`
	DocumentNumber string     `json:"document_number,omitempty"`
	Reason         string     `json:"reason,omitempty"`

	ActorID uuid.UUID `json:"actor_id"`

	Before BucketSnapshotResponse `json:"before"`
	After  BucketSnapshotResponse `json:"after"`
	Delta  BucketDeltaResponse    `json:"delta"`

	OccurredAt time.Time `json:"occurred_at"`
}
