// Package service orchestrates the InventoryLedgerEntry aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and, specific to this module,
// NO STOCK LOGIC. The service does not compute quantities, does not read a
// balance to decide anything, and never touches an inventory position. It
// translates a reported movement into an immutable record and appends it.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Service records and reads inventory ledger entries.
type Service struct {
	repo repository.Repository

	clock port.Clock
	ids   port.IDGenerator
	tx    transaction.Manager
}

// Compile-time proof that the service can be wired straight into the integration
// seam without an adapter.
var _ InventoryEventSubscriber = (*Service)(nil)

// New builds the service.
func New(
	repo repository.Repository,
	clock port.Clock,
	ids port.IDGenerator,
	tx transaction.Manager,
) *Service {
	return &Service{repo: repo, clock: clock, ids: ids, tx: tx}
}

// actor resolves the tenant and the acting user together. Both come from the
// RequestContext, never from a request field, so a client cannot name a company
// or impersonate a user.
func (s *Service) actor(ctx context.Context) (companyID, userID uuid.UUID, err error) {
	rc := appcontext.From(ctx)
	if userID, err = rc.RequireUser(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if companyID, err = rc.RequireTenant(); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return companyID, userID, nil
}

// RecordMovement witnesses a stock transition by appending one immutable entry.
//
// It performs NO stock arithmetic beyond the delta the aggregate derives from the
// two snapshots it was given, and it never writes to an inventory position. The
// position has already changed by the time this is called; the ledger's whole job
// is to remember that it did.
func (s *Service) RecordMovement(ctx context.Context, req dto.RecordMovementRequest) (dto.LedgerEntryResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}

	movementType := entity.MovementType(req.MovementType)
	if !movementType.Valid() {
		return dto.LedgerEntryResponse{}, apperror.Validation("invalid movement type").
			WithOp("inventoryledger.service.RecordMovement")
	}

	movementContext, err := entity.NewMovementContext(
		req.ReferenceType, req.ReferenceID, req.DocumentNumber, req.Reason,
	)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}

	before, err := entity.NewBeforeBucket(
		req.Before.Available, req.Before.Reserved, req.Before.Allocated, req.Before.Quarantined,
	)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}
	after, err := entity.NewAfterBucket(
		req.After.Available, req.After.Reserved, req.After.Allocated, req.After.Quarantined,
	)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}

	// Business time is the caller's to state — a backdated correction records when
	// the stock actually moved. Only an unset value falls back to the clock, and
	// it comes from the injected clock so the service stays deterministic.
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = s.clock.Now()
	}

	entry, err := entity.NewLedgerEntry(
		s.ids.NewID(), companyID,
		req.PositionID, req.ProductID, req.WarehouseID, req.LocationID,
		req.LotNumber, req.SerialNumber, req.OwnerID,
		movementType, movementContext,
		actorID,
		before, after,
		occurredAt,
	)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}

	// Wrapped in a transaction so that when this is called INSIDE the inventory
	// operation's own transaction it joins that unit of work: the position change
	// and its ledger entry then commit or roll back together, and the ledger can
	// never disagree with the position it witnesses.
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		return s.repo.Append(ctx, entry)
	})
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}

	return mapper.ToResponse(entry), nil
}

// OnInventoryMovement satisfies InventoryEventSubscriber. It discards the
// response because a subscriber has no caller to return one to, but it does NOT
// discard the error: the Inventory module calls this inside a movement's own
// transaction and aborts that movement when it fails, which is the whole basis
// of "every movement appends exactly one entry".
//
// Because RecordMovement wraps its append in RunInTransaction, and that manager
// joins an existing transaction through a SAVEPOINT rather than opening a second
// one, the entry is written on the caller's connection and commits with it.
func (s *Service) OnInventoryMovement(ctx context.Context, movement dto.RecordMovementRequest) error {
	_, err := s.RecordMovement(ctx, movement)
	return err
}

// GetLedger returns one entry.
func (s *Service) GetLedger(ctx context.Context, ledgerID uuid.UUID) (dto.LedgerEntryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}
	entry, err := s.repo.FindByID(ctx, ledgerID, companyID)
	if err != nil {
		return dto.LedgerEntryResponse{}, err
	}
	return mapper.ToResponse(entry), nil
}

// ListLedger returns a filtered, paginated page of the company's entries.
func (s *Service) ListLedger(ctx context.Context, query dto.ListLedgerQuery) (pagination.Page[dto.LedgerEntryResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}

	from, err := query.ParseOccurredFrom()
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	to, err := query.ParseOccurredTo()
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	if from != nil && to != nil && !to.After(*from) {
		return pagination.Page[dto.LedgerEntryResponse]{}, apperror.Validation(
			"occurred_to must be after occurred_from").
			WithOp("inventoryledger.service.ListLedger")
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging:        query.Request,
		PositionID:    parseID(query.PositionID),
		ProductID:     parseID(query.ProductID),
		WarehouseID:   parseID(query.WarehouseID),
		LocationID:    parseID(query.LocationID),
		MovementType:  query.MovementType,
		ReferenceType: query.ReferenceType,
		ReferenceID:   parseID(query.ReferenceID),
		OccurredFrom:  from,
		OccurredTo:    to,
	})
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// ListByPosition returns one position's movement history, newest first.
func (s *Service) ListByPosition(
	ctx context.Context, positionID uuid.UUID, paging pagination.Request,
) (pagination.Page[dto.LedgerEntryResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	if err := paging.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}

	page, err := s.repo.FindByPosition(ctx, companyID, positionID, paging)
	if err != nil {
		return pagination.Page[dto.LedgerEntryResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// ListByReference returns every entry caused by one business document.
func (s *Service) ListByReference(
	ctx context.Context, referenceType string, referenceID uuid.UUID,
) ([]dto.LedgerEntryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := s.repo.FindByReference(ctx, companyID, referenceType, referenceID)
	if err != nil {
		return nil, err
	}

	out := make([]dto.LedgerEntryResponse, 0, len(entries))
	for _, entry := range entries {
		out = append(out, mapper.ToResponse(entry))
	}
	return out, nil
}

// parseID converts a validated uuid string into a uuid, or uuid.Nil when empty.
func parseID(raw string) uuid.UUID {
	if raw == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}
	return id
}
