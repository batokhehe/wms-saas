package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func zapNop() *zap.Logger { return zap.NewNop() }

var errInfrastructure = errors.New("database unreachable")

// fakeRepo is an in-memory repository that reimplements tenant filtering and the
// code-uniqueness rule in Go, so an isolation or uniqueness bug in the service is
// caught here. The SQL is verified by the integration suite.
type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Customer
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Customer{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, c *entity.Customer) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byID {
		if e.CompanyID() == c.CompanyID() && strings.EqualFold(e.Code().String(), c.Code().String()) {
			return apperror.Conflict("duplicate code").WithOp("fake.Save")
		}
	}
	r.byID[c.ID()] = c
	return nil
}

func (r *fakeRepo) Update(_ context.Context, c *entity.Customer) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[c.ID()]; !ok {
		return apperror.NotFound("customer not found").WithOp("fake.Update")
	}
	r.byID[c.ID()] = c
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, customerID, companyID uuid.UUID) (*entity.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.byID[customerID]
	if !ok || c.CompanyID() != companyID {
		return nil, apperror.NotFound("customer not found").WithOp("fake.FindByID")
	}
	return c, nil
}

func (r *fakeRepo) FindByCode(_ context.Context, companyID uuid.UUID, code string) (*entity.Customer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.byID {
		if c.CompanyID() == companyID && strings.EqualFold(c.Code().String(), code) {
			return c, nil
		}
	}
	return nil, apperror.NotFound("customer not found").WithOp("fake.FindByCode")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Customer], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.Customer
	for _, c := range r.byID {
		if c.CompanyID() != companyID {
			continue
		}
		if filter.Status != "" && c.Status().String() != filter.Status {
			continue
		}
		matched = append(matched, c)
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, companyID uuid.UUID, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.byID {
		if c.CompanyID() == companyID && strings.EqualFold(c.Code().String(), code) {
			return true, nil
		}
	}
	return false, nil
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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.Customer {
	snap := map[uuid.UUID]*entity.Customer{}
	if m.repo == nil {
		return snap
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, c := range m.repo.byID {
		snap[id] = c
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.Customer) {
	if m.repo == nil {
		return
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.Customer{}
	for id, c := range snap {
		m.repo.byID[id] = c
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
