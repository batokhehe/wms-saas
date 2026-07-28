package service

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func zapNop() *zap.Logger { return zap.NewNop() }

var errInfrastructure = errors.New("database unreachable")

// ---------- repository ----------

// fakeRepo is an in-memory repository that reimplements tenant filtering and the
// three uniqueness rules in Go, so an isolation or uniqueness bug in the service
// is caught here. The SQL itself is verified by the persistence integration
// suite; this stands in for it.
type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Inventory
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Inventory{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, inv *entity.Inventory) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[inv.ID()]; ok {
		r.byID[inv.ID()] = inv // update
		return nil
	}
	// Create: enforce the per-tracking-type uniqueness the indexes enforce.
	for _, e := range r.byID {
		if inv.TrackingType() == entity.TrackingSerial && e.HasSerial() &&
			e.Serial().String() == inv.Serial().String() {
			return apperror.Conflict("duplicate serial").WithOp("fake.Save") // global
		}
		if e.CompanyID() != inv.CompanyID() {
			continue
		}
		samePlace := e.ProductID() == inv.ProductID() && e.LocationID() == inv.LocationID()
		if inv.TrackingType() == entity.TrackingNone && e.TrackingType() == entity.TrackingNone && samePlace {
			return apperror.Conflict("duplicate none position").WithOp("fake.Save")
		}
		if inv.TrackingType() == entity.TrackingLot && e.TrackingType() == entity.TrackingLot &&
			samePlace && e.Lot().String() == inv.Lot().String() {
			return apperror.Conflict("duplicate lot").WithOp("fake.Save")
		}
	}
	r.byID[inv.ID()] = inv
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id, companyID uuid.UUID) (*entity.Inventory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv, ok := r.byID[id]
	if !ok || inv.CompanyID() != companyID {
		return nil, apperror.NotFound("inventory not found").WithOp("fake.FindByID")
	}
	return inv, nil
}

func (r *fakeRepo) FindByProductLocation(_ context.Context, companyID, productID, locationID uuid.UUID) ([]*entity.Inventory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*entity.Inventory
	for _, inv := range r.byID {
		if inv.CompanyID() == companyID && inv.ProductID() == productID && inv.LocationID() == locationID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (r *fakeRepo) FindByLot(_ context.Context, companyID, productID, locationID uuid.UUID, lot string) (*entity.Inventory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.byID {
		if inv.CompanyID() == companyID && inv.ProductID() == productID &&
			inv.LocationID() == locationID && inv.Lot().String() == lot {
			return inv, nil
		}
	}
	return nil, apperror.NotFound("not found").WithOp("fake.FindByLot")
}

func (r *fakeRepo) FindBySerial(_ context.Context, companyID, productID uuid.UUID, serial string) (*entity.Inventory, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.byID {
		if inv.CompanyID() == companyID && inv.ProductID() == productID && inv.Serial().String() == serial {
			return inv, nil
		}
	}
	return nil, apperror.NotFound("not found").WithOp("fake.FindBySerial")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Inventory], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.Inventory
	for _, inv := range r.byID {
		if inv.CompanyID() != companyID {
			continue
		}
		if filter.Status != "" && inv.Status().String() != filter.Status {
			continue
		}
		if filter.Tracking != "" && inv.TrackingType().String() != filter.Tracking {
			continue
		}
		matched = append(matched, inv)
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) Exists(_ context.Context, companyID, productID, locationID uuid.UUID) (bool, error) {
	items, _ := r.FindByProductLocation(context.Background(), companyID, productID, locationID)
	return len(items) > 0, nil
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---------- transaction manager ----------

type fakeTxManager struct {
	repo      *fakeRepo
	calls     int
	rollbacks int
	depth     int
}

func (m *fakeTxManager) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.calls++
	if m.depth > 0 {
		return fn(ctx)
	}
	snapshot := m.snapshot()
	m.depth++
	err := fn(ctx)
	m.depth--
	if err != nil {
		m.rollbacks++
		m.restore(snapshot)
		return err
	}
	return nil
}

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.Inventory {
	snap := map[uuid.UUID]*entity.Inventory{}
	if m.repo == nil {
		return snap
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, inv := range m.repo.byID {
		snap[id] = inv
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.Inventory) {
	if m.repo == nil {
		return
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.Inventory{}
	for id, inv := range snap {
		m.repo.byID[id] = inv
	}
}

// ---------- event publisher ----------

type fakeEventPublisher struct {
	mu     sync.Mutex
	events []entity.Event
}

func (p *fakeEventPublisher) Publish(_ context.Context, event entity.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *fakeEventPublisher) has(name entity.EventName) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.events {
		if e.Name == name {
			return true
		}
	}
	return false
}

func (p *fakeEventPublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func (p *fakeEventPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

// ---------- providers ----------

type rejectingProductProvider struct{ calls int }

func (p *rejectingProductProvider) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error {
	p.calls++
	return apperror.Validation("no such product")
}

type rejectingWarehouseProvider struct{ calls int }

func (p *rejectingWarehouseProvider) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error {
	p.calls++
	return apperror.Validation("no such warehouse")
}

type rejectingLocationProvider struct{ calls int }

func (p *rejectingLocationProvider) VerifyLocation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	p.calls++
	return apperror.Validation("no such location")
}

// blockingReservationProvider reports active reservations, standing in for the
// future Reservation module refusing a stock removal.
type blockingReservationProvider struct{ calls int }

func (p *blockingReservationProvider) HasActiveReservations(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	p.calls++
	return true, nil
}
