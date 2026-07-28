package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/mapper"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/internal/shared/port"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Service orchestrates the StorageLocation aggregate.
//
// Read every method and notice what is absent: no `if status ==`, no capacity
// arithmetic, no state machine. Those live in the aggregate. What is here is
// the shape every method shares:
//
//	resolve tenant and actor → load aggregate → gather cross-aggregate FACTS
//	  → call ONE domain method → persist → publish
//
// The "gather facts" step is what distinguishes this service from the warehouse
// one. Two of this aggregate's rules — capacity and mixed-SKU — depend on data
// the aggregate cannot see, so the service fetches it and hands it over. The
// RULE stays in the domain; only the FACT comes from outside.
type Service struct {
	repo       repository.Repository
	warehouses WarehouseVerifier
	capacity   CurrentCapacityProvider
	inventory  InventoryProvider
	receiving  ReceivingGuard
	picking    PickingGuard
	counting   CycleCountGuard

	clock  port.Clock
	ids    port.IDGenerator
	tx     transaction.Manager
	events EventPublisher
}

// Dependencies bundles the service's collaborators.
//
// A struct rather than eleven positional parameters: a constructor with that
// many arguments of similar types is one where a caller silently transposes two
// guards, and the compiler cannot help because they are all interfaces.
type Dependencies struct {
	Repo       repository.Repository
	Warehouses WarehouseVerifier
	Capacity   CurrentCapacityProvider
	Inventory  InventoryProvider
	Receiving  ReceivingGuard
	Picking    PickingGuard
	Counting   CycleCountGuard

	Clock  port.Clock
	IDs    port.IDGenerator
	Tx     transaction.Manager
	Events EventPublisher
}

// New builds the service.
func New(deps Dependencies) *Service {
	return &Service{
		repo:       deps.Repo,
		warehouses: deps.Warehouses,
		capacity:   deps.Capacity,
		inventory:  deps.Inventory,
		receiving:  deps.Receiving,
		picking:    deps.Picking,
		counting:   deps.Counting,
		clock:      deps.Clock,
		ids:        deps.IDs,
		tx:         deps.Tx,
		events:     deps.Events,
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

// Create defines a location within a warehouse.
func (s *Service) Create(
	ctx context.Context, req dto.CreateLocationRequest,
) (dto.LocationResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	// Value objects validate themselves; the service never inspects a string.
	coordinate, err := entity.NewCoordinate(req.Zone, req.Aisle, req.Rack, req.Level, req.Bin)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	var explicitCode entity.LocationCode
	if req.Code != "" {
		explicitCode, err = entity.NewLocationCode(req.Code)
		if err != nil {
			return dto.LocationResponse{}, err
		}
	}

	barcode, err := entity.NewBarcode(req.Barcode)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	capacity, err := buildCapacity(req.MaxWeight, req.MaxVolume, req.MaxPallet)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	// Cross-aggregate check: the warehouse must exist in this company. Asked
	// through an interface rather than a repository call, because a location
	// aggregate that loaded a warehouse aggregate would collapse the boundary
	// that lets each be modified independently.
	if err := s.warehouses.VerifyWarehouse(ctx, companyID, req.WarehouseID); err != nil {
		return dto.LocationResponse{}, err
	}

	var response dto.LocationResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := entity.NewStorageLocation(
			s.ids.NewID(), companyID, req.WarehouseID,
			coordinate, explicitCode, actorID, s.clock.Now(),
		)
		if err != nil {
			return err
		}

		// Set-level rules: only the repository can see siblings. Checked
		// explicitly so the common case produces a clear message; the unique
		// indexes remain the real guarantee against a race.
		if err := s.assertCodeAvailable(
			ctx, companyID, req.WarehouseID, location.Code().String(), uuid.Nil,
		); err != nil {
			return err
		}

		if barcode.IsPresent() {
			if err := s.assertBarcodeAvailable(
				ctx, companyID, barcode.String(), uuid.Nil,
			); err != nil {
				return err
			}
			if err := location.AssignBarcode(barcode, actorID, s.clock.Now()); err != nil {
				return err
			}
		}

		if err := s.applyCreateOptions(location, req, actorID); err != nil {
			return err
		}

		if !capacity.IsUnlimited() {
			// A brand-new location holds nothing, so no provider call is needed
			// — passing the zero Usage is both correct and one fewer round trip.
			if err := location.ChangeCapacity(
				capacity, entity.Usage{}, actorID, s.clock.Now(),
			); err != nil {
				return err
			}
		}

		if err := s.repo.Save(ctx, location); err != nil {
			return err
		}

		response = mapper.ToResponse(location)
		s.publish(ctx, location)
		return nil
	})
	if err != nil {
		return dto.LocationResponse{}, err
	}

	return response, nil
}

// applyCreateOptions applies the optional flags supplied at creation.
func (s *Service) applyCreateOptions(
	location *entity.StorageLocation, req dto.CreateLocationRequest, actorID uuid.UUID,
) error {
	now := s.clock.Now()

	if req.PickingPriority != nil {
		if err := location.ChangePickingPriority(*req.PickingPriority, actorID, now); err != nil {
			return err
		}
	}

	if req.AllowMixedSKU != nil && *req.AllowMixedSKU {
		if err := location.EnableMixedSKU(actorID, now); err != nil {
			return err
		}
	}

	if req.AllowOverflow != nil {
		if err := location.SetAllowOverflow(*req.AllowOverflow, actorID, now); err != nil {
			return err
		}
	}

	return nil
}

// Get returns one location.
func (s *Service) Get(
	ctx context.Context, locationID uuid.UUID,
) (dto.LocationResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	location, err := s.repo.FindByID(ctx, locationID, companyID)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	return mapper.ToResponse(location), nil
}

// GetByBarcode resolves a scanned label.
//
// Not warehouse-scoped: a scanner reads a barcode with no idea which site it is
// standing in, and requiring a warehouse would make the lookup impossible for
// the case it exists to serve.
func (s *Service) GetByBarcode(
	ctx context.Context, barcode string,
) (dto.LocationResponse, error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	location, err := s.repo.FindByBarcode(ctx, companyID, barcode)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	return mapper.ToResponse(location), nil
}

// List returns a page of the company's locations.
func (s *Service) List(
	ctx context.Context, query dto.ListLocationsQuery,
) (pagination.Page[dto.LocationResponse], error) {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return pagination.Page[dto.LocationResponse]{}, err
	}

	// Apply resolves the sort key against the endpoint's allow-list. It must run
	// before the query reaches the repository; the base repository refuses an
	// unapplied request.
	if err := query.Request.Apply(dto.SortOptions()); err != nil {
		return pagination.Page[dto.LocationResponse]{}, err
	}

	filter := repository.ListFilter{
		Paging: query.Request,
		Status: query.Status,
		Zone:   query.Zone,
	}
	if query.WarehouseID != nil {
		filter.WarehouseID = *query.WarehouseID
	}

	page, err := s.repo.List(ctx, companyID, filter)
	if err != nil {
		return pagination.Page[dto.LocationResponse]{}, err
	}

	return mapper.ToPage(page), nil
}

// Update applies a partial update.
//
// Each supplied field becomes a separate CALL on the aggregate rather than a
// bulk assignment. That is the difference between a domain model and a record:
// DisableMixedSKU can refuse while two SKUs are stored, whereas assigning a
// struct field cannot.
func (s *Service) Update(
	ctx context.Context, locationID uuid.UUID, req dto.UpdateLocationRequest,
) (dto.LocationResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	var response dto.LocationResponse

	// Read-modify-write must be atomic: without a transaction, two concurrent
	// updates both read the same row and the second silently overwrites the
	// first's changes.
	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := s.load(ctx, locationID, companyID, "Update")
		if err != nil {
			return err
		}

		now := s.clock.Now()

		if req.PickingPriority != nil {
			if err := location.ChangePickingPriority(*req.PickingPriority, actorID, now); err != nil {
				return err
			}
		}

		if req.AllowOverflow != nil {
			if err := location.SetAllowOverflow(*req.AllowOverflow, actorID, now); err != nil {
				return err
			}
		}

		if req.AllowMixedSKU != nil {
			if err := s.applyMixedSKU(ctx, location, *req.AllowMixedSKU, companyID, actorID, now); err != nil {
				return err
			}
		}

		if err := s.repo.Update(ctx, location); err != nil {
			return err
		}

		response = mapper.ToResponse(location)
		s.publish(ctx, location)
		return nil
	})
	if err != nil {
		return dto.LocationResponse{}, s.concurrentModification(ctx, locationID, companyID, err)
	}

	return response, nil
}

// applyMixedSKU enables or disables the mixed-SKU policy.
//
// Disabling NARROWS a rule, so the aggregate needs to know what is stored:
// turning it off while two SKUs are present would leave the location
// permanently non-compliant with no way for the system to say so. Enabling
// widens and needs no fact at all, which is why the provider is consulted only
// on the disable path.
func (s *Service) applyMixedSKU(
	ctx context.Context,
	location *entity.StorageLocation,
	allow bool,
	companyID, actorID uuid.UUID,
	now time.Time,
) error {
	if allow {
		return location.EnableMixedSKU(actorID, now)
	}

	distinct, err := s.inventory.DistinctSKUs(ctx, companyID, location.ID())
	if err != nil {
		return err
	}

	return location.DisableMixedSKU(distinct, actorID, now)
}

// ChangeCapacity updates what a location can hold.
//
// This is where the aggregate's central rule is served. The service fetches the
// current usage from CurrentCapacityProvider and passes it in; the aggregate
// decides whether the new capacity accommodates it.
//
// The fetch happens INSIDE the transaction so the usage and the write are
// consistent — reading it outside would leave a window in which stock arrives
// between the check and the update.
func (s *Service) ChangeCapacity(
	ctx context.Context, locationID uuid.UUID, req dto.ChangeCapacityRequest,
) (dto.LocationResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	capacity, err := buildCapacity(req.MaxWeight, req.MaxVolume, req.MaxPallet)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	var response dto.LocationResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := s.load(ctx, locationID, companyID, "ChangeCapacity")
		if err != nil {
			return err
		}

		usage, err := s.capacity.CurrentUsage(ctx, companyID, locationID)
		if err != nil {
			return err
		}

		if err := location.ChangeCapacity(capacity, usage, actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, location); err != nil {
			return err
		}

		response = mapper.ToResponse(location)
		s.publish(ctx, location)
		return nil
	})
	if err != nil {
		return dto.LocationResponse{}, s.concurrentModification(ctx, locationID, companyID, err)
	}

	return response, nil
}

// AssignBarcode attaches or replaces a scannable label.
func (s *Service) AssignBarcode(
	ctx context.Context, locationID uuid.UUID, req dto.AssignBarcodeRequest,
) (dto.LocationResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	barcode, err := entity.NewBarcode(req.Barcode)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	var response dto.LocationResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := s.load(ctx, locationID, companyID, "AssignBarcode")
		if err != nil {
			return err
		}

		if barcode.IsPresent() {
			if err := s.assertBarcodeAvailable(
				ctx, companyID, barcode.String(), locationID,
			); err != nil {
				return err
			}
		}

		if err := location.AssignBarcode(barcode, actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, location); err != nil {
			return err
		}

		response = mapper.ToResponse(location)
		s.publish(ctx, location)
		return nil
	})
	if err != nil {
		return dto.LocationResponse{}, s.concurrentModification(ctx, locationID, companyID, err)
	}

	return response, nil
}

// Activate returns a location to service.
func (s *Service) Activate(
	ctx context.Context, locationID uuid.UUID,
) (dto.LocationResponse, error) {
	return s.mutate(ctx, locationID, "Activate",
		func(l *entity.StorageLocation, actorID uuid.UUID, now time.Time) error {
			return l.Activate(actorID, now)
		})
}

// Deactivate stands a location down.
func (s *Service) Deactivate(
	ctx context.Context, locationID uuid.UUID,
) (dto.LocationResponse, error) {
	return s.mutate(ctx, locationID, "Deactivate",
		func(l *entity.StorageLocation, actorID uuid.UUID, now time.Time) error {
			return l.Deactivate(actorID, now)
		})
}

// Lock places a location under an operational hold.
func (s *Service) Lock(
	ctx context.Context, locationID uuid.UUID, req dto.LockLocationRequest,
) (dto.LocationResponse, error) {
	return s.mutate(ctx, locationID, "Lock",
		func(l *entity.StorageLocation, actorID uuid.UUID, now time.Time) error {
			return l.Lock(req.Reason, actorID, now)
		})
}

// Unlock lifts an operational hold.
func (s *Service) Unlock(
	ctx context.Context, locationID uuid.UUID,
) (dto.LocationResponse, error) {
	return s.mutate(ctx, locationID, "Unlock",
		func(l *entity.StorageLocation, actorID uuid.UUID, now time.Time) error {
			return l.Unlock(actorID, now)
		})
}

// StartMaintenance schedules work on a location.
func (s *Service) StartMaintenance(
	ctx context.Context, locationID uuid.UUID,
) (dto.LocationResponse, error) {
	return s.mutate(ctx, locationID, "StartMaintenance",
		func(l *entity.StorageLocation, actorID uuid.UUID, now time.Time) error {
			return l.StartMaintenance(actorID, now)
		})
}

// Archive retires a location.
//
// A location is never hard-deleted: future stock movements will reference it
// forever, and erasing the row would orphan the history of what was stored
// where.
//
// The InventoryProvider is consulted BEFORE the aggregate, and that order
// matters. It answers the cross-aggregate question the aggregate cannot see;
// asking it second would mean archiving a location and then discovering it
// should not have been.
func (s *Service) Archive(ctx context.Context, locationID uuid.UUID) error {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return err
	}

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := s.load(ctx, locationID, companyID, "Archive")
		if err != nil {
			return err
		}

		// Extension point. EmptyInventory today; the Inventory sprint replaces
		// it with a real stock check and no code here changes.
		empty, err := s.inventory.IsEmpty(ctx, companyID, locationID)
		if err != nil {
			return err
		}
		if !empty {
			return apperror.Conflict("This location still holds stock and cannot be archived").
				WithOp("location.service.Archive")
		}

		if err := location.Archive(actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, location); err != nil {
			return err
		}

		s.publish(ctx, location)
		return nil
	})
	return s.concurrentModification(ctx, locationID, companyID, err)
}

// CanReceive reports whether stock may be put away into a location.
//
// It composes the two halves: the aggregate answers the LOCAL question (is this
// location ACTIVE?) and ReceivingGuard answers the cross-aggregate one. Neither
// can be skipped, and neither has to know about the other.
//
// Exposed as a query rather than used internally because nothing in this sprint
// receives stock — it exists so the Receiving sprint has a single call to make
// rather than reimplementing the composition.
func (s *Service) CanReceive(ctx context.Context, locationID uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}

	location, err := s.repo.FindByID(ctx, locationID, companyID)
	if err != nil {
		return err
	}

	if !location.CanReceiveInventory() {
		return apperror.Conflict("This location cannot receive inventory").
			WithOp("location.service.CanReceive").
			WithDetails(map[string]any{"status": location.Status().String()})
	}

	return s.receiving.CanReceive(ctx, companyID, locationID)
}

// CanPick reports whether stock may be taken from a location.
func (s *Service) CanPick(ctx context.Context, locationID uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}

	location, err := s.repo.FindByID(ctx, locationID, companyID)
	if err != nil {
		return err
	}

	if !location.CanPickInventory() {
		return apperror.Conflict("This location cannot be picked from").
			WithOp("location.service.CanPick").
			WithDetails(map[string]any{"status": location.Status().String()})
	}

	return s.picking.CanPick(ctx, companyID, locationID)
}

// CanCount reports whether a cycle count may be performed on a location.
//
// It does NOT consult the aggregate's availability predicates, deliberately: a
// location is often LOCKED *in order to* count it, so applying the receiving
// rules would refuse exactly when a count is most needed.
func (s *Service) CanCount(ctx context.Context, locationID uuid.UUID) error {
	companyID, _, err := s.actor(ctx)
	if err != nil {
		return err
	}

	if _, err := s.repo.FindByID(ctx, locationID, companyID); err != nil {
		return err
	}

	return s.counting.CanCount(ctx, companyID, locationID)
}

// ---------- internals ----------

// load fetches an aggregate and re-asserts tenancy.
//
// The repository already filtered by company, so the BelongsTo check is defence
// in depth — it can only fire if that filter is ever broken, which is exactly
// when a cross-tenant write would otherwise go unnoticed.
func (s *Service) load(
	ctx context.Context, locationID, companyID uuid.UUID, op string,
) (*entity.StorageLocation, error) {
	location, err := s.repo.FindByID(ctx, locationID, companyID)
	if err != nil {
		return nil, err
	}

	if !location.BelongsTo(companyID) {
		return nil, apperror.Forbidden("This location belongs to another company").
			WithOp("location.service." + op)
	}

	return location, nil
}

// mutate is the shared shape of every single-operation lifecycle method.
//
// Load, call one domain method, persist, publish — all inside one transaction.
// Factoring it out means the transaction boundary and the event publication are
// identical for every operation rather than re-derived six times, and a new
// lifecycle operation cannot forget either.
func (s *Service) mutate(
	ctx context.Context,
	locationID uuid.UUID,
	op string,
	apply func(*entity.StorageLocation, uuid.UUID, time.Time) error,
) (dto.LocationResponse, error) {
	companyID, actorID, err := s.actor(ctx)
	if err != nil {
		return dto.LocationResponse{}, err
	}

	var response dto.LocationResponse

	err = s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
		location, err := s.load(ctx, locationID, companyID, op)
		if err != nil {
			return err
		}

		if err := apply(location, actorID, s.clock.Now()); err != nil {
			return err
		}

		if err := s.repo.Update(ctx, location); err != nil {
			return err
		}

		response = mapper.ToResponse(location)
		s.publish(ctx, location)
		return nil
	})
	if err != nil {
		return dto.LocationResponse{}, s.concurrentModification(ctx, locationID, companyID, err)
	}

	return response, nil
}

// concurrentModification translates the repository's conditional-write miss
// into a retryable 409 and exposes only the winning version and entity ID.
func (s *Service) concurrentModification(ctx context.Context, id, companyID uuid.UUID, err error) error {
	if !errors.Is(err, sharedrepo.ErrConcurrentModification) {
		return err
	}

	current, readErr := s.repo.FindByID(ctx, id, companyID)
	if readErr != nil {
		return err
	}
	return apperror.Conflict("The resource was changed by another request").
		WithOp("location.service.concurrentModification").
		WithDetails(map[string]any{"entity_id": id, "current_version": current.Version()}).
		WithCause(sharedrepo.ErrConcurrentModification)
}

// assertCodeAvailable rejects a duplicate code within the warehouse.
func (s *Service) assertCodeAvailable(
	ctx context.Context, companyID, warehouseID uuid.UUID, code string, _ uuid.UUID,
) error {
	taken, err := s.repo.ExistsByCode(ctx, companyID, warehouseID, code)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("A location with this code already exists in this warehouse").
			WithOp("location.service.assertCodeAvailable")
	}
	return nil
}

// assertBarcodeAvailable rejects a duplicate barcode within the company.
//
// excludeID is the location being relabelled, so re-assigning a location its own
// current barcode is not a conflict. uuid.Nil means "exclude nothing", which is
// the create case.
func (s *Service) assertBarcodeAvailable(
	ctx context.Context, companyID uuid.UUID, barcode string, excludeID uuid.UUID,
) error {
	taken, err := s.repo.ExistsByBarcodeExcluding(ctx, companyID, barcode, excludeID)
	if err != nil {
		return err
	}
	if taken {
		return apperror.Conflict("This barcode is already assigned to another location").
			WithOp("location.service.assertBarcodeAvailable")
	}
	return nil
}

// publish drains the aggregate's recorded events and publishes them.
//
// PullEvents clears as it reads, so calling this twice cannot republish. The
// service never constructs an event itself — it only forwards what the
// aggregate recorded, which is why an event exists exactly when a transition
// happened.
func (s *Service) publish(ctx context.Context, l *entity.StorageLocation) {
	for _, event := range l.PullEvents() {
		s.events.Publish(ctx, event)
	}
}

// buildCapacity assembles a Capacity value object from request strings.
func buildCapacity(maxWeight, maxVolume string, maxPallet *int) (entity.Capacity, error) {
	weight, err := entity.NewQuantity(maxWeight, "max_weight")
	if err != nil {
		return entity.Capacity{}, err
	}

	volume, err := entity.NewQuantity(maxVolume, "max_volume")
	if err != nil {
		return entity.Capacity{}, err
	}

	return entity.NewCapacity(weight, volume, maxPallet)
}
