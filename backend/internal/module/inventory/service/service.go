// Package service orchestrates the Inventory aggregate.
//
// LAYER RULE: no gin, no gorm, no http, no SQL — and NO BUSINESS RULES. Every
// invariant lives in entity.Inventory. The service loads an aggregate, gathers
// any cross-aggregate FACTS the operation needs (provider verification,
// specifications), calls ONE domain method, persists and publishes.
//
// The shape every method shares:
//
//	resolve tenant and actor → load aggregate → gather cross-aggregate facts
//	  → call ONE domain method → persist → (commit) → publish
//
// It never re-checks an aggregate invariant. "Is there enough available to
// reserve?" and "is on-hand below reserved?" are the aggregate's; the service
// only supplies what the aggregate cannot see for itself.
package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/port"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/specification"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedport "github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// mutation is a single domain call on a loaded aggregate.
type mutation func(context.Context, *entity.Inventory, uuid.UUID) error

// Service orchestrates the Inventory aggregate.
type Service struct {
	repo repository.Repository

	inventoryExists specification.InventoryExists
	uniqueLot       specification.UniqueLot
	uniqueSerial    specification.UniqueSerial

	products     port.ProductProvider
	warehouses   port.WarehouseProvider
	locations    port.LocationProvider
	reservations port.ReservationProvider

	clock  sharedport.Clock
	ids    sharedport.IDGenerator
	tx     transaction.Manager
	events EventPublisher
}

// New builds the service, deriving its specifications from the repository.
func New(
	repo repository.Repository,
	products port.ProductProvider,
	warehouses port.WarehouseProvider,
	locations port.LocationProvider,
	reservations port.ReservationProvider,
	clock sharedport.Clock,
	ids sharedport.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	return &Service{
		repo:            repo,
		inventoryExists: specification.NewInventoryExists(repo),
		uniqueLot:       specification.NewUniqueLot(repo),
		uniqueSerial:    specification.NewUniqueSerial(repo),
		products:        products,
		warehouses:      warehouses,
		locations:       locations,
		reservations:    reservations,
		clock:           clock,
		ids:             ids,
		tx:              tx,
		events:          events,
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

// CreateInventory opens a stock position.
func (s *Service) CreateInventory(
	ctx context.Context, req dto.CreateInventoryRequest,
) (dto.InventoryResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.InventoryResponse{}, err
	}

	tracking := entity.TrackingType(req.Tracking)

	lot := entity.NoLotNumber()
	if req.LotNumber != nil && *req.LotNumber != "" {
		lot, err = entity.NewLotNumber(*req.LotNumber)
		if err != nil {
			return dto.InventoryResponse{}, err
		}
	}
	serial := entity.NoSerialNumber()
	if req.SerialNumber != nil && *req.SerialNumber != "" {
		serial, err = entity.NewSerialNumber(*req.SerialNumber)
		if err != nil {
			return dto.InventoryResponse{}, err
		}
	}
	initial, err := entity.NewInventoryQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}

	var inv *entity.Inventory

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Cross-aggregate references are verified through the providers — the
		// aggregate cannot load a warehouse, location or product to check them.
		if err := s.warehouses.VerifyWarehouse(ctx, companyID, req.WarehouseID); err != nil {
			return err
		}
		if err := s.locations.VerifyLocation(ctx, companyID, req.WarehouseID, req.LocationID); err != nil {
			return err
		}
		if err := s.products.VerifyProduct(ctx, companyID, req.ProductID); err != nil {
			return err
		}

		// Set-level uniqueness: the specification gives the clear message; the
		// partial unique indexes remain the race-proof backstop.
		if err := s.assertUnique(ctx, companyID, req, tracking, lot, serial); err != nil {
			return err
		}

		created, err := entity.NewInventory(
			s.ids.NewID(), companyID, req.WarehouseID, req.LocationID, req.ProductID,
			tracking, lot, serial, initial, actorID, s.clock.Now(),
		)
		if err != nil {
			return err
		}
		if err := s.repo.Save(ctx, created); err != nil {
			return err
		}
		inv = created
		return nil
	})
	if err != nil {
		return dto.InventoryResponse{}, err
	}

	s.publish(ctx, inv)
	return mapper.ToResponse(inv), nil
}

// assertUnique runs the tracking-appropriate uniqueness specification.
func (s *Service) assertUnique(
	ctx context.Context, companyID uuid.UUID, req dto.CreateInventoryRequest,
	tracking entity.TrackingType, lot entity.LotNumber, serial entity.SerialNumber,
) error {
	switch tracking {
	case entity.TrackingNone:
		exists, err := s.inventoryExists.Holds(ctx, companyID, req.ProductID, req.LocationID)
		if err != nil {
			return err
		}
		if exists {
			return apperror.Conflict("a stock position already exists for this product in this location").
				WithOp("inventory.service.CreateInventory")
		}
		return nil
	case entity.TrackingLot:
		return s.uniqueLot.Ensure(ctx, companyID, req.ProductID, req.LocationID, lot.String())
	case entity.TrackingSerial:
		return s.uniqueSerial.Ensure(ctx, companyID, req.ProductID, serial.String())
	default:
		return nil
	}
}

// GetInventory returns one stock position.
func (s *Service) GetInventory(ctx context.Context, inventoryID uuid.UUID) (dto.InventoryResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	inv, err := s.repo.FindByID(ctx, inventoryID, companyID)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return mapper.ToResponse(inv), nil
}

// ListInventory returns a page of the company's stock positions.
func (s *Service) ListInventory(
	ctx context.Context, query dto.ListInventoriesQuery,
) (pagination.Page[dto.InventoryResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.InventoryResponse]{}, err
	}
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.InventoryResponse]{}, err
	}

	filter := repository.ListFilter{
		Paging:   query.Request,
		Tracking: query.Tracking,
		Status:   query.Status,
	}
	// The id filters were bound as strings and validated as uuids; parse is safe.
	filter.WarehouseID = parseID(query.WarehouseID)
	filter.LocationID = parseID(query.LocationID)
	filter.ProductID = parseID(query.ProductID)

	page, err := s.repo.List(ctx, companyID, filter)
	if err != nil {
		return pagination.Page[dto.InventoryResponse]{}, err
	}
	return mapper.ToPage(page), nil
}

// IncreaseInventory adds received or found stock.
func (s *Service) IncreaseInventory(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "IncreaseInventory", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.Increase(amount, actor, s.clock.Now())
	})
}

// DecreaseInventory removes stock. It first consults the ReservationProvider —
// stock with an active EXTERNAL reservation (which the aggregate's own reserved
// count cannot see) must not be removed. Today the default reports none.
func (s *Service) DecreaseInventory(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "DecreaseInventory", func(ctx context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		if err := s.assertNoExternalReservations(ctx, inv); err != nil {
			return err
		}
		return inv.Decrease(amount, actor, s.clock.Now())
	})
}

// ReserveInventory promises available stock to an order.
func (s *Service) ReserveInventory(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "ReserveInventory", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.Reserve(amount, actor, s.clock.Now())
	})
}

// ReleaseReservation returns promised stock to the available pool.
func (s *Service) ReleaseReservation(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "ReleaseReservation", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.ReleaseReservation(amount, actor, s.clock.Now())
	})
}

// AdjustInventory sets on-hand to an absolute corrected value.
func (s *Service) AdjustInventory(ctx context.Context, id uuid.UUID, req dto.AdjustInventoryRequest) (dto.InventoryResponse, error) {
	counted, err := entity.NewInventoryQuantity(*req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "AdjustInventory", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.Adjust(counted, req.Reason, actor, s.clock.Now())
	})
}

// TransferOut removes available stock moving to another location.
func (s *Service) TransferOut(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "TransferOut", func(ctx context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		if err := s.assertNoExternalReservations(ctx, inv); err != nil {
			return err
		}
		return inv.TransferOut(amount, actor, s.clock.Now())
	})
}

// TransferIn adds stock arriving from another location.
func (s *Service) TransferIn(ctx context.Context, id uuid.UUID, req dto.QuantityRequest) (dto.InventoryResponse, error) {
	amount, err := entity.NewQuantity(req.Quantity)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "TransferIn", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.TransferIn(amount, actor, s.clock.Now())
	})
}

// LockInventory freezes a position.
func (s *Service) LockInventory(ctx context.Context, id uuid.UUID) (dto.InventoryResponse, error) {
	return s.mutate(ctx, id, "LockInventory", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.Lock(actor, s.clock.Now())
	})
}

// UnlockInventory returns a position to ACTIVE.
func (s *Service) UnlockInventory(ctx context.Context, id uuid.UUID) (dto.InventoryResponse, error) {
	return s.mutate(ctx, id, "UnlockInventory", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.Unlock(actor, s.clock.Now())
	})
}

// CompleteCycleCount reconciles on-hand to a physically counted value.
func (s *Service) CompleteCycleCount(ctx context.Context, id uuid.UUID, req dto.CycleCountRequest) (dto.InventoryResponse, error) {
	counted, err := entity.NewInventoryQuantity(*req.Counted)
	if err != nil {
		return dto.InventoryResponse{}, err
	}
	return s.mutate(ctx, id, "CompleteCycleCount", func(_ context.Context, inv *entity.Inventory, actor uuid.UUID) error {
		return inv.CompleteCycleCount(counted, actor, s.clock.Now())
	})
}

// mutate is the shared shape of every load-modify-persist operation.
//
// Load, run one domain call, persist — all inside one transaction — then publish
// AFTER the commit. Publishing after commit means an event is never emitted for a
// change that rolled back.
func (s *Service) mutate(
	ctx context.Context, id uuid.UUID, op string, apply mutation,
) (dto.InventoryResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.InventoryResponse{}, err
	}

	var inv *entity.Inventory

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		loaded, err := s.repo.FindByID(ctx, id, companyID)
		if err != nil {
			return err
		}
		// Defence in depth. The repository already filtered by tenant, so this
		// can only fire if that filter is ever broken.
		if loaded.CompanyID() != companyID {
			return apperror.Forbidden("This inventory belongs to another company").
				WithOp("inventory.service." + op)
		}
		if err := apply(ctx, loaded, actorID); err != nil {
			return err
		}
		if err := s.repo.Save(ctx, loaded); err != nil {
			return err
		}
		inv = loaded
		return nil
	})
	if err != nil {
		return dto.InventoryResponse{}, s.concurrentModification(ctx, id, companyID, err)
	}

	s.publish(ctx, inv)
	return mapper.ToResponse(inv), nil
}

// assertNoExternalReservations blocks removing stock that a future Reservation
// aggregate holds. It is the cross-aggregate guard the inventory cannot answer
// itself; the default provider reports none, so it never blocks today.
func (s *Service) assertNoExternalReservations(ctx context.Context, inv *entity.Inventory) error {
	held, err := s.reservations.HasActiveReservations(ctx, inv.CompanyID(), inv.ID())
	if err != nil {
		return err
	}
	if held {
		return apperror.Conflict("stock cannot be removed while active reservations reference it").
			WithOp("inventory.service.assertNoExternalReservations")
	}
	return nil
}

// concurrentModification turns the repository sentinel into the API's normal 409
// after the transaction has rolled back. A fresh read returns the winning
// writer's version, the token a client needs before retrying.
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
// clears as it reads, so calling this twice cannot republish. The service never
// constructs an event itself — it forwards what the aggregate recorded.
func (s *Service) publish(ctx context.Context, inv *entity.Inventory) {
	for _, event := range inv.PullEvents() {
		s.events.Publish(ctx, event)
	}
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
