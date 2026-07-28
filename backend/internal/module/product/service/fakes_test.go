package service

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func zapNop() *zap.Logger { return zap.NewNop() }

// In-memory doubles for the repository, the transaction manager, the event
// publisher and the four verifiers.
//
// The repository fake reimplements tenant filtering and the three uniqueness
// rules in Go. That is deliberate: a fake that ignored the companyID argument
// would make every cross-company isolation test pass while the real query
// leaked, so it enforces the same rules the scopes and indexes do. The SQL is
// verified separately by the integration suite.

// ---------- repository ----------

type fakeRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*entity.Product
	failOn map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[uuid.UUID]*entity.Product{},
		failOn: map[string]error{},
	}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Save(_ context.Context, p *entity.Product) error {
	if err := r.failOn["Save"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Mirrors ux_products_company_sku / _name and ux_product_barcodes_company_barcode.
	for _, existing := range r.byID {
		if existing.CompanyID() != p.CompanyID() {
			continue
		}
		if strings.EqualFold(existing.SKU().String(), p.SKU().String()) ||
			strings.EqualFold(strings.TrimSpace(existing.Name().String()), strings.TrimSpace(p.Name().String())) {
			return apperror.Conflict("duplicate").WithOp("fake.Save")
		}
		if barcodesCollide(existing, p) {
			return apperror.Conflict("duplicate barcode").WithOp("fake.Save")
		}
	}

	r.byID[p.ID()] = p
	return nil
}

func (r *fakeRepo) Update(_ context.Context, p *entity.Product) error {
	if err := r.failOn["Update"]; err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byID[p.ID()]; !ok {
		return apperror.NotFound("product not found").WithOp("fake.Update")
	}
	r.byID[p.ID()] = p
	return nil
}

func (r *fakeRepo) FindByID(
	_ context.Context, productID, companyID uuid.UUID,
) (*entity.Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.byID[productID]
	// The companyID check IS the tenant filter under test.
	if !ok || p.CompanyID() != companyID {
		return nil, apperror.NotFound("product not found").WithOp("fake.FindByID")
	}
	return p, nil
}

func (r *fakeRepo) FindBySKU(
	_ context.Context, companyID uuid.UUID, sku string,
) (*entity.Product, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.byID {
		if p.CompanyID() == companyID && strings.EqualFold(p.SKU().String(), sku) {
			return p, nil
		}
	}
	return nil, apperror.NotFound("product not found").WithOp("fake.FindBySKU")
}

func (r *fakeRepo) List(
	_ context.Context, companyID uuid.UUID, filter repository.ListFilter,
) (pagination.Page[*entity.Product], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := make([]*entity.Product, 0)
	for _, p := range r.byID {
		if p.CompanyID() != companyID {
			continue
		}
		if filter.Status != "" && p.Status().String() != filter.Status {
			continue
		}
		if filter.Tracking != "" && p.TrackingMethod().String() != filter.Tracking {
			continue
		}
		matched = append(matched, p)
	}
	return pagination.NewPage(matched, filter.Paging, int64(len(matched))), nil
}

func (r *fakeRepo) ExistsBySKU(_ context.Context, companyID uuid.UUID, sku string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.byID {
		if p.CompanyID() == companyID && strings.EqualFold(p.SKU().String(), sku) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ExistsByName(_ context.Context, companyID uuid.UUID, name string) (bool, error) {
	return r.ExistsByNameExcluding(context.Background(), companyID, name, uuid.Nil)
}

func (r *fakeRepo) ExistsByNameExcluding(
	_ context.Context, companyID uuid.UUID, name string, excludeID uuid.UUID,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.byID {
		if p.ID() == excludeID || p.CompanyID() != companyID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.Name().String()), strings.TrimSpace(name)) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeRepo) ExistsByBarcode(_ context.Context, companyID uuid.UUID, barcode string) (bool, error) {
	return r.ExistsByBarcodeExcluding(context.Background(), companyID, barcode, uuid.Nil)
}

func (r *fakeRepo) ExistsByBarcodeExcluding(
	_ context.Context, companyID uuid.UUID, barcode string, excludeProductID uuid.UUID,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.byID {
		if p.ID() == excludeProductID || p.CompanyID() != companyID {
			continue
		}
		for _, b := range p.Barcodes() {
			if strings.EqualFold(strings.TrimSpace(b.Barcode().String()), strings.TrimSpace(barcode)) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// barcodesCollide reports whether two products share any barcode (case
// insensitive), mirroring the company-scoped unique index on product_barcodes.
func barcodesCollide(a, b *entity.Product) bool {
	set := map[string]struct{}{}
	for _, x := range a.Barcodes() {
		set[strings.ToLower(strings.TrimSpace(x.Barcode().String()))] = struct{}{}
	}
	for _, y := range b.Barcodes() {
		if _, ok := set[strings.ToLower(strings.TrimSpace(y.Barcode().String()))]; ok {
			return true
		}
	}
	return false
}

// ---------- transaction manager ----------

// fakeTxManager simulates real transaction semantics, including ROLLBACK. It
// snapshots the repository before running fn and restores it on error, so a
// failed flow does not commit partial work in the fake.
type fakeTxManager struct {
	repo *fakeRepo

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

func (m *fakeTxManager) snapshot() map[uuid.UUID]*entity.Product {
	snap := map[uuid.UUID]*entity.Product{}
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

func (m *fakeTxManager) restore(snap map[uuid.UUID]*entity.Product) {
	if m.repo == nil {
		return
	}
	m.repo.mu.Lock()
	defer m.repo.mu.Unlock()
	m.repo.byID = map[uuid.UUID]*entity.Product{}
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

// ---------- verifiers ----------

// rejectingUOMVerifier stands in for a UOM module that refuses an unknown unit.
type rejectingUOMVerifier struct{ calls int }

var _ UOMVerifier = (*rejectingUOMVerifier)(nil)

func (v *rejectingUOMVerifier) VerifyUOM(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return apperror.NewValidation(apperror.FieldError{
		Field: "uom_id", Rule: "not_found", Message: "no such unit of measure in this company",
	}).WithOp("fake.UOMVerifier")
}

// countingUOMVerifier accepts but records, so a test can assert the service
// consults it.
type countingUOMVerifier struct{ calls int }

var _ UOMVerifier = (*countingUOMVerifier)(nil)

func (v *countingUOMVerifier) VerifyUOM(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return nil
}

// armableUOMVerifier accepts until reject is set, so a test can let the
// base-unit check at create succeed and then have a later AddUOM fail.
type armableUOMVerifier struct {
	reject bool
	calls  int
}

var _ UOMVerifier = (*armableUOMVerifier)(nil)

func (v *armableUOMVerifier) VerifyUOM(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	if v.reject {
		return apperror.NewValidation(apperror.FieldError{
			Field: "uom_id", Rule: "not_found", Message: "no such unit of measure in this company",
		}).WithOp("fake.UOMVerifier")
	}
	return nil
}

// rejectingCategoryVerifier refuses an unknown category.
type rejectingCategoryVerifier struct{ calls int }

var _ CategoryVerifier = (*rejectingCategoryVerifier)(nil)

func (v *rejectingCategoryVerifier) VerifyCategory(context.Context, uuid.UUID, uuid.UUID) error {
	v.calls++
	return apperror.NewValidation(apperror.FieldError{
		Field: "category_id", Rule: "not_found", Message: "no such category in this company",
	}).WithOp("fake.CategoryVerifier")
}

// fakeInventoryProvider reports a configurable presence of stock, standing in
// for the Inventory sprint's implementation.
type fakeInventoryProvider struct {
	has   bool
	calls int
}

var _ InventoryProvider = (*fakeInventoryProvider)(nil)

func (p *fakeInventoryProvider) HasInventory(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	p.calls++
	return p.has, nil
}

var errInfrastructure = errors.New("database unreachable")
