// Package service orchestrates the InventoryPosition aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.InventoryPosition. The service resolves the tenant
// and actor, verifies cross-aggregate references through its verifiers, opens ONE
// transaction, calls ONE aggregate behaviour per aggregate, persists, commits and
// then publishes.
//
// The service never computes a quantity, never touches a bucket, and never
// bypasses an aggregate method.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedport "github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// movement is a single domain call on a loaded position.
type movement func(*entity.InventoryPosition, uuid.UUID, time.Time) error

// Service orchestrates the InventoryPosition aggregate.
type Service struct {
	repo repository.Repository

	products   ProductVerifier
	warehouses WarehouseVerifier
	locations  LocationVerifier
	policy     StockPolicyProvider

	clock  sharedport.Clock
	ids    sharedport.IDGenerator
	tx     transaction.Manager
	events EventPublisher
}

// New builds the service.
func New(
	repo repository.Repository,
	products ProductVerifier,
	warehouses WarehouseVerifier,
	locations LocationVerifier,
	policy StockPolicyProvider,
	clock sharedport.Clock,
	ids sharedport.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	if policy == nil {
		policy = NewDefaultStockPolicy()
	}
	return &Service{
		repo:       repo,
		products:   products,
		warehouses: warehouses,
		locations:  locations,
		policy:     policy,
		clock:      clock,
		ids:        ids,
		tx:         tx,
		events:     events,
	}
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

// ---------------------------------------------------------------------------
// Receive
// ---------------------------------------------------------------------------

// ReceiveStock books stock into a position, opening it on first receipt.
//
// Flow: validate product → validate warehouse → validate location →
// GetOrCreatePosition → Position.Receive → Update → publish.
func (s *Service) ReceiveStock(ctx context.Context, req dto.ReceiveStockRequest) (dto.PositionResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PositionResponse{}, err
	}

	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.PositionResponse{}, err
	}
	key, err := stockKeyFrom(companyID, req.StockKeyRequest)
	if err != nil {
		return dto.PositionResponse{}, err
	}

	var position *entity.InventoryPosition

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		if err := s.verifyKey(ctx, key); err != nil {
			return err
		}

		loaded, err := s.repo.GetOrCreatePosition(ctx, key, actorID)
		if err != nil {
			return err
		}
		if err := loaded.Receive(amount, actorID, s.clock.Now()); err != nil {
			return err
		}

		// Policy, not invariant: a company may require received stock to be held
		// for inspection before it becomes available. The aggregate's quarantine
		// bucket is the mechanism; the policy decides whether to use it.
		hold, err := s.policy.RequireQuarantineOnReceipt(ctx, companyID, key.ProductID())
		if err != nil {
			return err
		}
		if hold {
			if err := loaded.MoveToQuarantine(amount, actorID, s.clock.Now()); err != nil {
				return err
			}
		}

		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		position = loaded
		return nil
	})
	if err != nil {
		return dto.PositionResponse{}, err
	}

	s.publish(ctx, position)
	return mapper.ToResponse(position), nil
}

// ---------------------------------------------------------------------------
// Single-position movements
// ---------------------------------------------------------------------------

// IssueStock removes stock from a position's available balance.
func (s *Service) IssueStock(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "IssueStock",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Issue(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// ReserveStock softly promises available stock to an order.
func (s *Service) ReserveStock(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "ReserveStock",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Reserve(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// ReleaseReservation returns a soft promise to the available pool.
func (s *Service) ReleaseReservation(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "ReleaseReservation",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Release(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// AllocateStock hardens a reservation into an assignment against a task.
func (s *Service) AllocateStock(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "AllocateStock",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Allocate(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// DeallocateStock returns an assignment to the reserved pool.
func (s *Service) DeallocateStock(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "DeallocateStock",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Deallocate(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// MoveToQuarantine holds available stock back from use.
func (s *Service) MoveToQuarantine(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "MoveToQuarantine",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.MoveToQuarantine(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// ReleaseFromQuarantine returns held stock to the available pool.
func (s *Service) ReleaseFromQuarantine(ctx context.Context, req dto.PositionQuantityRequest) (dto.PositionResponse, error) {
	return s.moveQuantity(ctx, req, "ReleaseFromQuarantine",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.ReleaseFromQuarantine(entity.MustQuantity(req.Quantity), actor, now)
		})
}

// AdjustStock reconciles a position to a counted total, with the reason that
// justifies it. The reason is carried into the aggregate's event so an auditor
// can tell a cycle count from a shrinkage write-off.
func (s *Service) AdjustStock(ctx context.Context, req dto.AdjustStockRequest) (dto.PositionResponse, error) {
	if !req.Type.Valid() {
		return dto.PositionResponse{}, apperror.Validation("invalid adjustment type").
			WithOp("inventory.service.AdjustStock")
	}
	if req.Quantity == nil {
		return dto.PositionResponse{}, apperror.Validation("adjustment quantity is required").
			WithOp("inventory.service.AdjustStock")
	}

	reason := req.Reason
	if reason == "" {
		reason = req.Type.String()
	}
	counted := *req.Quantity

	return s.mutate(ctx, req.PositionID, "AdjustStock",
		func(p *entity.InventoryPosition, actor uuid.UUID, now time.Time) error {
			return p.Adjust(counted, reason, actor, now)
		})
}

// moveQuantity validates the amount, then applies a single-position movement.
// Validating here means MustQuantity inside each closure cannot panic.
func (s *Service) moveQuantity(
	ctx context.Context, req dto.PositionQuantityRequest, op string, apply movement,
) (dto.PositionResponse, error) {
	if _, err := entity.NewQuantity(req.Quantity); err != nil {
		return dto.PositionResponse{}, err
	}
	return s.mutate(ctx, req.PositionID, op, apply)
}

// ---------------------------------------------------------------------------
// Transfer
// ---------------------------------------------------------------------------

// TransferStock moves stock between two positions, atomically.
//
// Flow: BEGIN → load origin → derive the destination key → verify it →
// GetOrCreatePosition → Origin.Issue → Destination.Receive → Update both →
// COMMIT → publish both.
//
// Transfer is ORCHESTRATION, not an aggregate behaviour: it spans two positions,
// and no single aggregate may reach into another. The service composes the two
// halves the aggregate already provides — Issue on the origin, Receive on the
// destination — inside one transaction, so stock can never be removed from the
// origin without landing at the destination.
func (s *Service) TransferStock(ctx context.Context, req dto.TransferStockRequest) (dto.PositionResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PositionResponse{}, err
	}

	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.PositionResponse{}, err
	}

	var origin, destination *entity.InventoryPosition

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loadedOrigin, err := s.repo.FindByID(ctx, req.FromPositionID, companyID)
		if err != nil {
			return err
		}
		if loadedOrigin.CompanyID() != companyID {
			return apperror.Forbidden("This position belongs to another company").
				WithOp("inventory.service.TransferStock")
		}
		if loadedOrigin.LocationID() == req.ToLocationID {
			return apperror.Validation("transfer destination must differ from the origin location").
				WithOp("inventory.service.TransferStock")
		}

		// Same product, same attributes, new place: the destination key is the
		// origin's key relocated, so a transfer can never change what the stock is.
		destinationKey, err := loadedOrigin.Key().WithLocation(req.ToWarehouseID, req.ToLocationID)
		if err != nil {
			return err
		}
		if err := s.verifyKey(ctx, destinationKey); err != nil {
			return err
		}

		loadedDestination, err := s.repo.GetOrCreatePosition(ctx, destinationKey, actorID)
		if err != nil {
			return err
		}

		now := s.clock.Now()
		if err := loadedOrigin.Issue(amount, actorID, now); err != nil {
			return err
		}
		if err := loadedDestination.Receive(amount, actorID, now); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, loadedOrigin); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, loadedDestination); err != nil {
			return err
		}

		origin, destination = loadedOrigin, loadedDestination
		return nil
	})
	if err != nil {
		return dto.PositionResponse{}, s.concurrentModification(ctx, req.FromPositionID, companyID, err)
	}

	// Published only after the commit, so a rolled-back transfer emits nothing.
	s.publish(ctx, origin)
	s.publish(ctx, destination)

	return mapper.ToResponse(origin), nil
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

// GetInventoryPosition returns one position.
func (s *Service) GetInventoryPosition(ctx context.Context, positionID uuid.UUID) (dto.PositionResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.PositionResponse{}, err
	}
	position, err := s.repo.FindByID(ctx, positionID, companyID)
	if err != nil {
		return dto.PositionResponse{}, err
	}
	return mapper.ToResponse(position), nil
}

// ListInventoryPositions returns a page of the company's positions.
func (s *Service) ListInventoryPositions(ctx context.Context, query dto.ListPositionsQuery) (pagination.Page[dto.PositionResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.PositionResponse]{}, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.PositionResponse]{}, err
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging:      query.Request,
		WarehouseID: parseID(query.WarehouseID),
		LocationID:  parseID(query.LocationID),
		ProductID:   parseID(query.ProductID),
		Tracking:    query.Tracking,
	})
	if err != nil {
		return pagination.Page[dto.PositionResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// ---------------------------------------------------------------------------
// Shared orchestration
// ---------------------------------------------------------------------------

// mutate is the shared shape of every load-modify-persist operation on ONE
// position: load, call one aggregate behaviour, persist — inside one transaction
// — then publish after the commit.
func (s *Service) mutate(
	ctx context.Context, positionID uuid.UUID, op string, apply movement,
) (dto.PositionResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.PositionResponse{}, err
	}

	var position *entity.InventoryPosition

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.repo.FindByID(ctx, positionID, companyID)
		if err != nil {
			return err
		}
		// Defence in depth: the repository already filtered by tenant, so this can
		// only fire if that filter is ever broken.
		if loaded.CompanyID() != companyID {
			return apperror.Forbidden("This position belongs to another company").
				WithOp("inventory.service." + op)
		}
		if err := apply(loaded, actorID, s.clock.Now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, loaded); err != nil {
			return err
		}
		position = loaded
		return nil
	})
	if err != nil {
		return dto.PositionResponse{}, s.concurrentModification(ctx, positionID, companyID, err)
	}

	s.publish(ctx, position)
	return mapper.ToResponse(position), nil
}

// verifyKey runs the three external verifiers for a stock key. The aggregate
// cannot load a product, warehouse or location, so these answers come from
// outside — but only as an error or nil, never as a foreign aggregate.
func (s *Service) verifyKey(ctx context.Context, key entity.StockKey) error {
	if err := s.warehouses.VerifyWarehouse(ctx, key.CompanyID(), key.WarehouseID()); err != nil {
		return err
	}
	if err := s.locations.VerifyLocation(ctx, key.CompanyID(), key.WarehouseID(), key.LocationID()); err != nil {
		return err
	}
	return s.products.VerifyProduct(ctx, key.CompanyID(), key.ProductID())
}

// concurrentModification turns the repository sentinel into the API's normal 409
// after rollback, carrying the current version as the retry token.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if err == nil || !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}
	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("inventory.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// publish drains the aggregate's recorded events and publishes them. PullEvents
// clears as it reads, so a double publish is impossible; the service never
// constructs an event itself.
func (s *Service) publish(ctx context.Context, p *entity.InventoryPosition) {
	if p == nil {
		return
	}
	for _, event := range p.PullEvents() {
		s.events.Publish(ctx, event)
	}
}

// stockKeyFrom builds a validated StockKey from a transport request. The tenant
// comes from the RequestContext, never from the body.
func stockKeyFrom(companyID uuid.UUID, req dto.StockKeyRequest) (entity.StockKey, error) {
	lot := entity.NoLotNumber()
	if req.LotNumber != nil && *req.LotNumber != "" {
		built, err := entity.NewLotNumber(*req.LotNumber)
		if err != nil {
			return entity.StockKey{}, err
		}
		lot = built
	}
	serial := entity.NoSerialNumber()
	if req.SerialNumber != nil && *req.SerialNumber != "" {
		built, err := entity.NewSerialNumber(*req.SerialNumber)
		if err != nil {
			return entity.StockKey{}, err
		}
		serial = built
	}

	attrs, err := entity.NewStockAttributes(entity.TrackingType(req.Tracking), lot, serial)
	if err != nil {
		return entity.StockKey{}, err
	}
	return entity.NewStockKey(companyID, req.WarehouseID, req.LocationID, req.ProductID, attrs)
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
