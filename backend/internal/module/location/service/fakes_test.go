package service

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// In-memory doubles for the repository, the transaction manager, the event
// publisher and all six extension points.
//
// The repository fake reimplements tenant AND warehouse scoping in Go. That is
// deliberate: a fake that ignored those arguments would make every isolation
// test pass while the real query leaked, so it enforces the same rules the
// scopes do. The SQL is verified separately by the integration suite.

// ---------- repository ----------

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.StorageLocation
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[uuid.UUID]*entity.StorageLocation{},
		failOn: map[string]error{},
	}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, l *entity.StorageLocation) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors ux_storage_locations_warehouse_code and _company_barcode.
	for _, existing := range r.byID {
		if existing.IsArchived() {
			continue
		}
		if existing.WarehouseID() == l.WarehouseID() &&
			strings.EqualFold(existing.Code().String(), l.Code().String()) {
			return apperror.Conflict("duplicate code").WithOp("fake.Save")
		}
		if existing.CompanyID() == l.CompanyID() &&
			l.Barcode().IsPresent() &&
			existing.Barcode().String() == l.Barcode().String() {
			return apperror.Conflict("duplicate barcode").WithOp("fake.Save")
		}
	}

	r.byID[l.ID()] = l
	return nil
}

func (r *fakeRepo) Update(_ context.Context, l *entity.StorageLocation) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[l.ID()]; !ok {
		return apperror.NotFound("location not found").WithOp("fake.Update")
	}
	r.byID[l.ID()] = l
	return nil
}

func (r *fakeRepo) SaveMany(ctx context.Context, locations []*entity.StorageLocation) error {
	for _, l := range locations {
		if err := r.Save(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeRepo) FindByID(
	_ context.Context, locationID, companyID uuid.UUID,
) (*entity.StorageLocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	l, ok := r.byID[locationID]
	// The companyID check IS the tenant filter under test.
	if !ok || l.CompanyID() != companyID {
		return nil, apperror.NotFound("location not found").WithOp("fake.FindByID")
	}
	return l, nil
}

func (r *fakeRepo) FindByCode(
	_ context.Context, companyID, warehouseID uuid.UUID, code string,
) (*entity.StorageLocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, l := range r.byID {
		if l.CompanyID() == companyID && l.WarehouseID() == warehouseID &&
			strings.EqualFold(l.Code().String(), code) {
			return l, nil
		}
	}
	return nil, apperror.NotFound("location not found").WithOp("fake.FindByCode")
}

func (r *fakeRepo) FindByBarcode(
	_ context.Context, companyID uuid.UUID, barcode string,
) (*entity.StorageLocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	value := strings.TrimSpace(barcode)
	for _, l := range r.byID {
		// Compared exactly, matching the real query: a barcode is a machine
		// token, and folding case would let two distinct labels resolve to one
		// location.
		if l.CompanyID() == companyID && l.Barcode().String() == value &&
			l.Barcode().IsPresent() {
			return l, nil
		}
	}
	return nil, apperror.NotFound("location not found").WithOp("fake.FindByBarcode")
}

func (r *fakeRepo) List(
	_ context.Context, companyID uuid.UUID, filter repository.ListFilter,
) (pagination.Page[*entity.StorageLocation], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]*entity.StorageLocation, 0)
	for _, l := range r.byID {
		if l.CompanyID() != companyID {
			continue
		}
		if filter.WarehouseID != uuid.Nil && l.WarehouseID() != filter.WarehouseID {
			continue
		}
		if filter.Status != "" && l.Status().String() != filter.Status {
			continue
		}
		if filter.Zone != "" && !strings.EqualFold(l.Coordinate().Zone(), filter.Zone) {
			continue
		}
		matched = append(matched, l)
	}

	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsByCode(
	_ context.Context, companyID, warehouseID uuid.UUID, code string,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, l := range r.byID {
		if l.IsArchived() {
			continue
		}
		if l.CompanyID() == companyID && l.WarehouseID() == warehouseID &&
			strings.EqualFold(l.Code().String(), code) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ExistsByBarcode(
	ctx context.Context, companyID uuid.UUID, barcode string,
) (bool, error) {
	return r.ExistsByBarcodeExcluding(ctx, companyID, barcode, uuid.Nil)
}

func (r *fakeRepo) ExistsByBarcodeExcluding(
	_ context.Context, companyID uuid.UUID, barcode string, excludeID uuid.UUID,
) (bool, error) {
	value := strings.TrimSpace(barcode)
	if value == "" {
		return false, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, l := range r.byID {
		if l.ID() == excludeID || l.IsArchived() || l.CompanyID() != companyID {
			continue
		}
		if l.Barcode().String() == value {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) CountByWarehouse(
	_ context.Context, companyID, warehouseID uuid.UUID,
) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var count int64
	for _, l := range r.byID {
		if l.CompanyID() == companyID && l.WarehouseID() == warehouseID {
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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.StorageLocation {
	snap := map[uuid.UUID]*entity.StorageLocation{}
	if m.repo == nil {
		return snap
	}

	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	for id, l := range m.repo.byID {
		snap[id] = l
	}
	return snap
}

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.StorageLocation) {
	if m.repo == nil {
		return
	}

	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.StorageLocation{}
	for id, l := range snap {
		m.repo.byID[id] = l
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

// ---------- extension-point doubles ----------
//
// Each stands in for a future module and proves the extension point works:
// substituting one changes behaviour without any file in this module changing.

// rejectingWarehouseVerifier stands in for a warehouse that does not exist.
type rejectingWarehouseVerifier struct{ calls int }

var _ WarehouseVerifier = (*rejectingWarehouseVerifier)(nil)

func (v *rejectingWarehouseVerifier) VerifyWarehouse(
	context.Context, uuid.UUID, uuid.UUID,
) error {
	v.calls++
	return ErrWarehouseNotFound()
}

// countingWarehouseVerifier accepts but records, so a test can assert the
// service consults it.
type countingWarehouseVerifier struct{ calls int }

var _ WarehouseVerifier = (*countingWarehouseVerifier)(nil)

func (v *countingWarehouseVerifier) VerifyWarehouse(
	context.Context, uuid.UUID, uuid.UUID,
) error {
	v.calls++
	return nil
}

// stubCapacity stands in for the Inventory sprint's CurrentCapacityProvider.
type stubCapacity struct {
	usage entity.Usage
	calls int
}

var _ CurrentCapacityProvider = (*stubCapacity)(nil)

func (p *stubCapacity) CurrentUsage(
	context.Context, uuid.UUID, uuid.UUID,
) (entity.Usage, error) {
	p.calls++
	return p.usage, nil
}

// stubInventory stands in for the Inventory sprint's InventoryProvider.
type stubInventory struct {
	distinctSKUs int
	empty        bool
	skuCalls     int
	emptyCalls   int
}

var _ InventoryProvider = (*stubInventory)(nil)

func (p *stubInventory) DistinctSKUs(
	context.Context, uuid.UUID, uuid.UUID,
) (int, error) {
	p.skuCalls++
	return p.distinctSKUs, nil
}

func (p *stubInventory) IsEmpty(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	p.emptyCalls++
	return p.empty, nil
}

// blockingReceivingGuard stands in for the Receiving sprint.
type blockingReceivingGuard struct{ calls int }

var _ ReceivingGuard = (*blockingReceivingGuard)(nil)

func (g *blockingReceivingGuard) CanReceive(
	context.Context, uuid.UUID, uuid.UUID,
) error {
	g.calls++
	return apperror.Conflict("no compatible product for this location").
		WithOp("fake.ReceivingGuard")
}

// blockingPickingGuard stands in for the Picking sprint.
type blockingPickingGuard struct{ calls int }

var _ PickingGuard = (*blockingPickingGuard)(nil)

func (g *blockingPickingGuard) CanPick(context.Context, uuid.UUID, uuid.UUID) error {
	g.calls++
	return apperror.Conflict("a pick wave already reserved this location").
		WithOp("fake.PickingGuard")
}

// blockingCycleCountGuard stands in for the Cycle Count sprint.
type blockingCycleCountGuard struct{ calls int }

var _ CycleCountGuard = (*blockingCycleCountGuard)(nil)

func (g *blockingCycleCountGuard) CanCount(context.Context, uuid.UUID, uuid.UUID) error {
	g.calls++
	return apperror.Conflict("a count is already in progress").
		WithOp("fake.CycleCountGuard")
}

func zapNop() *zap.Logger { return zap.NewNop() }
