package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/repository"
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
	byID   map[uuid.UUID]*entity.Supplier
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]*entity.Supplier{}, failOn: map[string]error{}}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, s *entity.Supplier) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byID {
		if e.CompanyID() == s.CompanyID() && strings.EqualFold(e.Code().String(), s.Code().String()) {
			return apperror.Conflict("duplicate code").WithOp("fake.Save")
		}
	}
	r.byID[s.ID()] = s
	return nil
}

func (r *fakeRepo) Update(_ context.Context, s *entity.Supplier) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[s.ID()]; !ok {
		return apperror.NotFound("supplier not found").WithOp("fake.Update")
	}
	r.byID[s.ID()] = s
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, supplierID, companyID uuid.UUID) (*entity.Supplier, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[supplierID]
	if !ok || s.CompanyID() != companyID {
		return nil, apperror.NotFound("supplier not found").WithOp("fake.FindByID")
	}
	return s, nil
}

func (r *fakeRepo) FindByCode(_ context.Context, companyID uuid.UUID, code string) (*entity.Supplier, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.byID {
		if s.CompanyID() == companyID && strings.EqualFold(s.Code().String(), code) {
			return s, nil
		}
	}
	return nil, apperror.NotFound("supplier not found").WithOp("fake.FindByCode")
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.Supplier], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.Supplier
	for _, s := range r.byID {
		if s.CompanyID() != companyID {
			continue
		}
		if filter.Status != "" && s.Status().String() != filter.Status {
			continue
		}
		matched = append(matched, s)
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(_ context.Context, companyID uuid.UUID, code string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.byID {
		if s.CompanyID() == companyID && strings.EqualFold(s.Code().String(), code) {
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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.Supplier {
	snap := map[uuid.UUID]*entity.Supplier{}
	if m.repo == nil {
		return snap
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, s := range m.repo.byID {
		snap[id] = s
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.Supplier) {
	if m.repo == nil {
		return
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.Supplier{}
	for id, s := range snap {
		m.repo.byID[id] = s
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
