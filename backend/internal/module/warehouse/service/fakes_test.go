package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// In-memory doubles for the repository, the transaction manager, the event
// publisher and both extension-point guards.
//
// The repository fake reimplements tenant filtering in Go. That is deliberate:
// a fake that ignored the companyID argument would make every cross-company
// isolation test pass while the real query leaked, so it enforces the same rule
// the scopes do. The SQL is verified separately by the integration suite.

// ---------- repository ----------

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Warehouse
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[uuid.UUID]*entity.Warehouse{},
		failOn: map[string]error{},
	}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, w *entity.Warehouse) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors ux_warehouses_company_code / _name.
	for _, existing := range r.byID {
		if existing.CompanyID() != w.CompanyID() || existing.IsArchived() {
			continue
		}
		if strings.EqualFold(existing.Code().String(), w.Code().String()) ||
			strings.EqualFold(existing.Name(), w.Name()) {
			return apperror.Conflict("duplicate").WithOp("fake.Save")
		}
	}

	r.byID[w.ID()] = w
	return nil
}

func (r *fakeRepo) Update(_ context.Context, w *entity.Warehouse) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[w.ID()]; !ok {
		return apperror.NotFound("warehouse not found").WithOp("fake.Update")
	}
	r.byID[w.ID()] = w
	return nil
}

func (r *fakeRepo) FindByID(
	_ context.Context, warehouseID, companyID uuid.UUID,
) (*entity.Warehouse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.byID[warehouseID]
	// The companyID check IS the tenant filter under test.
	if !ok || w.CompanyID() != companyID {
		return nil, apperror.NotFound("warehouse not found").WithOp("fake.FindByID")
	}
	return w, nil
}

func (r *fakeRepo) FindByCode(
	_ context.Context, companyID uuid.UUID, code string,
) (*entity.Warehouse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, w := range r.byID {
		if w.CompanyID() == companyID && strings.EqualFold(w.Code().String(), code) {
			return w, nil
		}
	}
	return nil, apperror.NotFound("warehouse not found").WithOp("fake.FindByCode")
}

func (r *fakeRepo) List(
	_ context.Context, companyID uuid.UUID, filter repository.ListFilter,
) (pagination.Page[*entity.Warehouse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]*entity.Warehouse, 0)
	for _, w := range r.byID {
		if w.CompanyID() != companyID {
			continue
		}
		if filter.Status != "" && w.Status().String() != filter.Status {
			continue
		}
		if filter.Type != "" && w.Type().String() != filter.Type {
			continue
		}
		matched = append(matched, w)
	}

	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(
	_ context.Context, companyID uuid.UUID, code string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, w := range r.byID {
		if w.CompanyID() == companyID && !w.IsArchived() &&
			strings.EqualFold(w.Code().String(), code) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ExistsByName(
	_ context.Context, companyID uuid.UUID, name string,
) (bool, error) {
	return r.ExistsByNameExcluding(context.Background(), companyID, name, uuid.Nil)
}

func (r *fakeRepo) ExistsByNameExcluding(
	_ context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, w := range r.byID {
		if w.ID() == excludeID || w.CompanyID() != companyID || w.IsArchived() {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(w.Name()), strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) CountActive(_ context.Context, companyID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, w := range r.byID {
		if w.CompanyID() == companyID && w.IsOperational() {
			count++
		}
	}
	return count, nil
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK.
//
// It snapshots the repository before running fn and restores it on error.
// Without this a failed flow would commit partial work in the fake, and a whole
// class of bug becomes invisible to unit tests — the auth sprint hit exactly
// that.
type fakeTxManager struct {
	repo *fakeRepo

	calls     int
	rollbacks int
	depth     int
}

func (m *fakeTxManager) RunInTransaction(
	ctx context.Context, fn func(context.Context) error,
) error {
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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.Warehouse {
	snap := map[uuid.UUID]*entity.Warehouse{}
	if m.repo == nil {
		return snap
	}

	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, w := range m.repo.byID {
		snap[id] = w
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.Warehouse) {
	if m.repo == nil {
		return
	}

	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.Warehouse{}
	for id, w := range snap {
		m.repo.byID[id] = w
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

func (p *fakeEventPublisher) names() []entity.EventName {
	p.mu.Lock()
	defer p.mu.Unlock()

	names := make([]entity.EventName, 0, len(p.events))
	for _, e := range p.events {
		names = append(names, e.Name)
	}
	return names
}

func (p *fakeEventPublisher) has(name entity.EventName) bool {
	for _, got := range p.names() {
		if got == name {
			return true
		}
	}
	return false
}

func (p *fakeEventPublisher) find(name entity.EventName) (entity.Event, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, e := range p.events {
		if e.Name == name {
			return e, true
		}
	}
	return entity.Event{}, false
}

func (p *fakeEventPublisher) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = nil
}

// ---------- extension-point guards ----------

// blockingDeletionGuard stands in for the Inventory sprint's implementation.
// It is the whole point of the extension point: a future module refuses an
// archive without any warehouse file changing.
type blockingDeletionGuard struct {
	reason string
	calls  int
}

var _ DeletionGuard = (*blockingDeletionGuard)(nil)

func (g *blockingDeletionGuard) CanDelete(
	context.Context, uuid.UUID, uuid.UUID,
) error {
	g.calls++
	return apperror.Conflict(g.reason).WithOp("fake.DeletionGuard")
}

// rejectingZoneVerifier stands in for the Location sprint's implementation.
type rejectingZoneVerifier struct {
	calls int
}

var _ ZoneVerifier = (*rejectingZoneVerifier)(nil)

func (v *rejectingZoneVerifier) VerifyZone(
	context.Context, uuid.UUID, uuid.UUID,
) error {
	v.calls++
	return apperror.NewValidation(apperror.FieldError{
		Field: "zone_id", Rule: "not_found",
		Message: "no such zone in this company",
	}).WithOp("fake.ZoneVerifier")
}

// countingZoneVerifier accepts but records, so a test can assert the service
// consults it.
type countingZoneVerifier struct{ calls int }

var _ ZoneVerifier = (*countingZoneVerifier)(nil)

func (v *countingZoneVerifier) VerifyZone(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return nil
}

func zapNop() *zap.Logger { return zap.NewNop() }

var errInfrastructure = errors.New("database unreachable")
