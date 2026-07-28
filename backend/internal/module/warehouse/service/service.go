package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Service orchestrates the Warehouse aggregate.
//
// Read every method below and notice what is absent: there is no `if status ==`
// anywhere, no readiness check, no state machine. Those live in the aggregate.
// What is here is the shape every method shares:
//
//	resolve tenant and actor → load aggregate → call ONE domain method
//	  → check set-level rules the aggregate cannot see → persist → publish
//
// The set-level checks (is this code taken?) sit here because an aggregate can
// only see itself; answering them would require loading every sibling.
type Service struct {
	repo     repository.Repository
	deletion DeletionGuard
	zones    ZoneVerifier
	clock    port.Clock
	ids      port.IDGenerator
	tx       transaction.Manager
	events   EventPublisher
}

// New builds the service.
func New(
	repo repository.Repository,
	deletion DeletionGuard,
	zones ZoneVerifier,
	clock port.Clock,
	ids port.IDGenerator,
	tx transaction.Manager,
	events EventPublisher,
) *Service {
	return &Service{
		repo:     repo,
		deletion: deletion,
		zones:    zones,
		clock:    clock,
		ids:      ids,
		tx:       tx,
		events:   events,
	}
}

// actor resolves the tenant and the acting user together.
//
// Every method starts here. Both come from the RequestContext, never from a
// request field, so a client cannot name a company or impersonate a user.
func (s *Service) actor(ctx context.Context) (companyID, userID uuid.UUID, err error) {
	rc := appcontext.From(ctx)

	userID, err = rc.RequireUser()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	companyID, err = rc.RequireTenant()
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return companyID, userID, nil
}

// Create registers a warehouse in DRAFT.
func (s *Service) Create(
	ctx context.Context, req dto.CreateWarehouseRequest,
) (dto.WarehouseResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	// The value object validates itself; the service never inspects the string.
	code, err := entity.NewCode(req.Code)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	var response dto.WarehouseResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		// Set-level rules: only the repository can see siblings. Checked
		// explicitly so the common case produces a clear message; the unique
		// indexes remain the real guarantee against a race.
		if err := s.assertCodeAvailable(ctx, companyID, code.String()); err != nil {
			return err
		}
		if err := s.assertNameAvailable(ctx, companyID, req.Name, uuid.Nil); err != nil {
			return err
		}

		// The FACTORY builds the aggregate — always in DRAFT, always raising
		// WarehouseCreated. The service cannot construct one any other way.
		warehouse, err := entity.NewWarehouse(
			s.ids.NewID(), companyID, code,
			req.Name, req.Description, entity.Type(req.Type),
			actorID, s.clock.Now(),
		)
		if err != nil {
			return err
		}

		if err := s.repo.Save(ctx, warehouse); err != nil {
			return err
		}

		response = mapper.ToResponse(warehouse)
		s.publish(ctx, warehouse)
		return nil
	})
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	return response, nil
}

// Get returns one warehouse.
func (s *Service) Get(
	ctx context.Context, warehouseID uuid.UUID,
) (dto.WarehouseResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	warehouse, err := s.repo.FindByID(ctx, warehouseID, companyID)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	return mapper.ToResponse(warehouse), nil
}

// List returns a page of the company's warehouses.
func (s *Service) List(
	ctx context.Context, query dto.ListWarehousesQuery,
) (pagination.Page[dto.WarehouseResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.WarehouseResponse]{}, err
	}

	// Apply resolves the sort key against the endpoint's allow-list. It must run
	// before the query reaches the repository; the base repository refuses an
	// unapplied request.
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.WarehouseResponse]{}, err
	}

	page, err := s.repo.List(ctx, companyID, repository.ListFilter{
		Paging: query.Request,
		Status: query.Status,
		Type:   query.Type,
	})
	if err != nil {
		return pagination.Page[dto.WarehouseResponse]{}, err
	}

	return mapper.ToPage(page), nil
}

// Update applies a partial update.
//
// Each supplied field becomes a separate CALL on the aggregate rather than a
// bulk assignment. That is the difference between a domain model and a record:
// `ChangeAddress` can refuse to clear the address of an ACTIVE warehouse,
// whereas assigning a struct field cannot.
func (s *Service) Update(
	ctx context.Context, warehouseID uuid.UUID, req dto.UpdateWarehouseRequest,
) (dto.WarehouseResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	var response dto.WarehouseResponse

	// Read-modify-write must be atomic: without a transaction, two concurrent
	// updates both read the same row and the second silently overwrites the
	// first's changes.
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		warehouse, err := s.repo.FindByID(ctx, warehouseID, companyID)
		if err != nil {
			return err
		}

		// Defence in depth. The repository already filtered by tenant, so this
		// can only fire if that filter is ever broken — which is exactly when a
		// cross-tenant write would otherwise go unnoticed.
		if !warehouse.BelongsTo(companyID) {
			return apperror.Forbidden("This warehouse belongs to another company").
				WithOp("warehouse.service.Update")
		}

		now := s.clock.Now()

		if req.Name != nil {
			if err := s.assertNameAvailable(ctx, companyID, *req.Name, warehouseID); err != nil {
				return err
			}
			if err := warehouse.ChangeName(*req.Name, actorID, now); err != nil {
				return err
			}
		}

		if req.Description != nil {
			if err := warehouse.ChangeDescription(*req.Description, actorID, now); err != nil {
				return err
			}
		}

		if req.Address != nil {
			address, err := entity.NewAddress(*req.Address)
			if err != nil {
				return err
			}
			if err := warehouse.ChangeAddress(address, actorID, now); err != nil {
				return err
			}
		}

		if err := s.applyZones(ctx, warehouse, req, companyID, actorID, now); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, warehouse); err != nil {
			return err
		}

		response = mapper.ToResponse(warehouse)
		s.publish(ctx, warehouse)
		return nil
	})
	if err != nil {
		return dto.WarehouseResponse{}, s.concurrentModification(ctx, warehouseID, companyID, err)
	}

	return response, nil
}

// applyZones assigns any zones named in an update request.
//
// Each is verified through ZoneVerifier before the aggregate is asked to assign
// it. That check cannot live in the aggregate — zones are another aggregate —
// so this is where the Location sprint plugs in without touching the domain.
func (s *Service) applyZones(
	ctx context.Context,
	warehouse *entity.Warehouse,
	req dto.UpdateWarehouseRequest,
	companyID, actorID uuid.UUID,
	now time.Time,
) error {
	assignments := []struct {
		id     *uuid.UUID
		assign func(uuid.UUID, uuid.UUID, time.Time) error
	}{
		{req.ReceivingZoneID, warehouse.AssignReceivingZone},
		{req.ShippingZoneID, warehouse.AssignShippingZone},
		{req.StagingZoneID, warehouse.AssignStagingZone},
	}

	for _, assignment := range assignments {
		if assignment.id == nil {
			continue
		}
		if err := s.zones.VerifyZone(ctx, companyID, *assignment.id); err != nil {
			return err
		}
		if err := assignment.assign(*assignment.id, actorID, now); err != nil {
			return err
		}
	}

	return nil
}

// ChangeContact updates who is responsible for a warehouse.
func (s *Service) ChangeContact(
	ctx context.Context, warehouseID uuid.UUID, req dto.ChangeContactRequest,
) (dto.WarehouseResponse, error) {
	contact, err := entity.NewContact(req.ContactName, req.ContactPhone)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	return s.mutate(ctx, warehouseID, "ChangeContact",
		func(w *entity.Warehouse, actorID uuid.UUID, now time.Time) error {
			return w.ChangeContact(contact, actorID, now)
		})
}

// Activate makes a warehouse operational.
//
// The readiness rules — name, address, contact, at least one zone — are not
// here. They are in entity.Warehouse.Activate, which is the only place that can
// guarantee they hold whenever the status becomes ACTIVE.
func (s *Service) Activate(
	ctx context.Context, warehouseID uuid.UUID,
) (dto.WarehouseResponse, error) {
	return s.mutate(ctx, warehouseID, "Activate",
		func(w *entity.Warehouse, actorID uuid.UUID, now time.Time) error {
			return w.Activate(actorID, now)
		})
}

// Deactivate stands a warehouse down.
func (s *Service) Deactivate(
	ctx context.Context, warehouseID uuid.UUID,
) (dto.WarehouseResponse, error) {
	return s.mutate(ctx, warehouseID, "Deactivate",
		func(w *entity.Warehouse, actorID uuid.UUID, now time.Time) error {
			return w.Deactivate(actorID, now)
		})
}

// Suspend places a warehouse under a hold.
func (s *Service) Suspend(
	ctx context.Context, warehouseID uuid.UUID, req dto.SuspendWarehouseRequest,
) (dto.WarehouseResponse, error) {
	return s.mutate(ctx, warehouseID, "Suspend",
		func(w *entity.Warehouse, actorID uuid.UUID, now time.Time) error {
			return w.Suspend(req.Reason, actorID, now)
		})
}

// Archive retires a warehouse.
//
// This is the ONLY removal operation, and both the DELETE endpoint and the
// PATCH /archive endpoint call it. A warehouse is never hard-deleted: future
// stock movements will reference it forever, and erasing the row would orphan
// operational history an audit depends on.
//
// The DeletionGuard is consulted BEFORE the aggregate, and that order matters.
// The guard answers the cross-aggregate question ("does it hold stock?") which
// the aggregate cannot see; asking it second would mean archiving a warehouse
// and then discovering it should not have been.
func (s *Service) Archive(ctx context.Context, warehouseID uuid.UUID) error {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return err
	}

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		warehouse, err := s.repo.FindByID(ctx, warehouseID, companyID)
		if err != nil {
			return err
		}

		// Extension point. AlwaysAllowDeletion today; the Inventory sprint
		// replaces it with a stock check and no code here changes.
		if err := s.deletion.CanDelete(ctx, companyID, warehouseID); err != nil {
			return err
		}

		if err := warehouse.Archive(actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, warehouse); err != nil {
			return err
		}

		s.publish(ctx, warehouse)
		return nil
	})
	return s.concurrentModification(ctx, warehouseID, companyID, err)
}

// mutate is the shared shape of every single-operation lifecycle method.
//
// Load, call one domain method, persist, publish — all inside one transaction.
// Factoring it out means the transaction boundary and the event publication are
// identical for every operation rather than re-derived five times, and a new
// lifecycle operation cannot forget either.
func (s *Service) mutate(
	ctx context.Context,
	warehouseID uuid.UUID,
	op string,
	apply func(*entity.Warehouse, uuid.UUID, time.Time) error,
) (dto.WarehouseResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.WarehouseResponse{}, err
	}

	var response dto.WarehouseResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		warehouse, err := s.repo.FindByID(ctx, warehouseID, companyID)
		if err != nil {
			return err
		}

		if !warehouse.BelongsTo(companyID) {
			return apperror.Forbidden("This warehouse belongs to another company").
				WithOp("warehouse.service." + op)
		}

		if err := apply(warehouse, actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, warehouse); err != nil {
			return err
		}

		response = mapper.ToResponse(warehouse)
		s.publish(ctx, warehouse)
		return nil
	})
	if err != nil {
		return dto.WarehouseResponse{}, s.concurrentModification(ctx, warehouseID, companyID, err)
	}

	return response, nil
}

// concurrentModification turns the repository sentinel into the API's normal
// 409 envelope after the transaction has rolled back. A fresh read returns the
// winning writer's version, which is the token a client needs before retrying.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}

	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("warehouse.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// assertCodeAvailable rejects a duplicate code within the company.
func (s *Service) assertCodeAvailable(
	ctx context.Context, companyID uuid.UUID, code string,
) error {
	taken, err := s.repo.ExistsByCode(ctx, companyID, code)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A warehouse with this code already exists").
			WithOp("warehouse.service.assertCodeAvailable")
	}
	return nil
}

// assertNameAvailable rejects a duplicate name within the company.
//
// excludeID is the warehouse being renamed, so renaming one to its own current
// name is not reported as a conflict. uuid.Nil means "exclude nothing", which is
// the create case.
func (s *Service) assertNameAvailable(
	ctx context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) error {
	var (
		taken bool
		err   error
	)

	if excludeID == uuid.Nil {
		taken, err = s.repo.ExistsByName(ctx, companyID, name)
	} else {
		taken, err = s.repo.ExistsByNameExcluding(ctx, companyID, name, excludeID)
	}
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A warehouse with this name already exists").
			WithOp("warehouse.service.assertNameAvailable")
	}
	return nil
}

// publish drains the aggregate's recorded events and publishes them.
//
// PullEvents clears as it reads, so calling this twice cannot republish. The
// service never constructs an event itself — it only forwards what the
// aggregate recorded, which is why an event exists exactly when a transition
// happened.
func (s *Service) publish(ctx context.Context, w *entity.Warehouse) {
	for _, event := range w.PullEvents() {
		s.events.Publish(ctx, event)
	}
}
