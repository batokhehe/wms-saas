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

// fakeRepo is an in-memory repository that reimplements tenant filtering and
// key-based lookup in Go, so an isolation or addressing bug in the service is
// caught here. The SQL itself belongs to the integration suite.
type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.InventoryPosition
	failOn map[string]error
	ids    func() uuid.UUID
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[uuid.UUID]*entity.InventoryPosition{},
		failOn: map[string]error{},
		ids:    uuid.New,
	}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

// GetOrCreatePosition mirrors the real contract: find by key, or open an EMPTY
// position that is not persisted until Update.
func (r *fakeRepo) GetOrCreatePosition(
	ctx context.Context, key entity.StockKey, actorID uuid.UUID,
) (*entity.InventoryPosition, error) {
	if err := r.failOn["GetOrCreatePosition"]; err != nil {
		return nil, err
	}
	found, err := r.FindByKey(ctx, key)
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, apperror.ErrNotFound) {
		return nil, err
	}
	return entity.NewInventoryPosition(r.ids(), key, actorID, fixedNow())
}

func (r *fakeRepo) Update(_ context.Context, p *entity.InventoryPosition) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID()] = p
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, positionID, companyID uuid.UUID) (*entity.InventoryPosition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[positionID]
	// The companyID check IS the tenant filter under test.
	if !ok || p.CompanyID() != companyID {
		return nil, apperror.NotFound("position not found").WithOp("fake.FindByID")
	}
	return p, nil
}

func (r *fakeRepo) FindByKey(_ context.Context, key entity.StockKey) (*entity.InventoryPosition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.byID {
		if p.Key().Equals(key) {
			return p, nil
		}
	}
	return nil, apperror.NotFound("position not found").WithOp("fake.FindByKey")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.InventoryPosition], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.InventoryPosition
	for _, p := range r.byID {
		if p.CompanyID() != companyID {
			continue
		}
		if filter.WarehouseID != uuid.Nil && p.WarehouseID() != filter.WarehouseID {
			continue
		}
		if filter.LocationID != uuid.Nil && p.LocationID() != filter.LocationID {
			continue
		}
		if filter.ProductID != uuid.Nil && p.ProductID() != filter.ProductID {
			continue
		}
		if filter.Tracking != "" && p.Attributes().Tracking().String() != filter.Tracking {
			continue
		}
		matched = append(matched, p)
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK: it
// snapshots the repository before running fn and restores it on error.
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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.InventoryPosition {
	snap := map[uuid.UUID]*entity.InventoryPosition{}
	if m.repo == nil {
		return snap
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, p := range m.repo.byID {
		snap[id] = p
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.InventoryPosition) {
	if m.repo == nil {
		return
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.InventoryPosition{}
	for id, p := range snap {
		m.repo.byID[id] = p
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

// ---------- verifiers ----------

type rejectingProductVerifier struct{ calls int }

func (v *rejectingProductVerifier) VerifyProduct(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return apperror.Validation("no such product")
}

type rejectingWarehouseVerifier struct{ calls int }

func (v *rejectingWarehouseVerifier) VerifyWarehouse(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return apperror.Validation("no such warehouse")
}

type rejectingLocationVerifier struct{ calls int }

func (v *rejectingLocationVerifier) VerifyLocation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	v.calls++
	return apperror.Validation("no such location")
}

// quarantineOnReceipt is a policy that always demands a hold on receipt.
type quarantineOnReceipt struct{ calls int }

func (p *quarantineOnReceipt) AllowNegativeStock(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}

func (p *quarantineOnReceipt) RequireQuarantineOnReceipt(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	p.calls++
	return true, nil
}
