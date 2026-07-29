package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventoryledger/repository"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

var errInfrastructure = errors.New("database unreachable")

// ---------- fakes ----------

// fakeRepo is an in-memory ledger. It reimplements tenant filtering, duplicate
// rejection and the list filters in Go, so an isolation or filtering bug in the
// service is caught here. Crucially it also has NO update path — mirroring the
// real contract, which is what "append-only" means at this layer.
type fakeRepo struct {
	mu      sync.Mutex
	entries []*entity.InventoryLedgerEntry
	failOn  map[string]error
}

var _ repository.Repository = (*fakeRepo)(nil)

func newFakeRepo() *fakeRepo {
	return &fakeRepo{failOn: map[string]error{}}
}

func (r *fakeRepo) fail(method string, err error) { r.failOn[method] = err }

func (r *fakeRepo) Append(_ context.Context, e *entity.InventoryLedgerEntry) error {
	if err := r.failOn["Append"]; err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// A repeated id is refused rather than overwriting history.
	for _, existing := range r.entries {
		if existing.LedgerID() == e.LedgerID() {
			return apperror.Conflict("duplicate ledger entry").WithOp("fake.Append")
		}
	}
	r.entries = append(r.entries, e)
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, ledgerID, companyID uuid.UUID) (*entity.InventoryLedgerEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		// The companyID check IS the tenant filter under test.
		if e.LedgerID() == ledgerID && e.CompanyID() == companyID {
			return e, nil
		}
	}
	return nil, apperror.NotFound("ledger entry not found").WithOp("fake.FindByID")
}

func (r *fakeRepo) FindByPosition(_ context.Context, companyID, positionID uuid.UUID, paging pagination.Request) (pagination.Page[*entity.InventoryLedgerEntry], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.InventoryLedgerEntry
	for _, e := range r.entries {
		if e.CompanyID() == companyID && e.PositionID() == positionID {
			matched = append(matched, e)
		}
	}
	sortNewestFirst(matched)
	return pagination.NewPage(paginate(matched, paging), paging, int64(len(matched))), nil
}

func (r *fakeRepo) FindByReference(_ context.Context, companyID uuid.UUID, referenceType string, referenceID uuid.UUID) ([]*entity.InventoryLedgerEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matched []*entity.InventoryLedgerEntry
	for _, e := range r.entries {
		if e.CompanyID() != companyID {
			continue
		}
		ref := e.ReferenceID()
		if ref == nil || *ref != referenceID {
			continue
		}
		if referenceType != "" && e.ReferenceType() != referenceType {
			continue
		}
		matched = append(matched, e)
	}
	return matched, nil
}

func (r *fakeRepo) List(_ context.Context, companyID uuid.UUID, filter repository.ListFilter) (pagination.Page[*entity.InventoryLedgerEntry], error) {
	if err := r.failOn["List"]; err != nil {
		return pagination.Page[*entity.InventoryLedgerEntry]{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var matched []*entity.InventoryLedgerEntry
	for _, e := range r.entries {
		if e.CompanyID() != companyID {
			continue
		}
		if filter.PositionID != uuid.Nil && e.PositionID() != filter.PositionID {
			continue
		}
		if filter.ProductID != uuid.Nil && e.ProductID() != filter.ProductID {
			continue
		}
		if filter.WarehouseID != uuid.Nil && e.WarehouseID() != filter.WarehouseID {
			continue
		}
		if filter.MovementType != "" && e.MovementType().String() != filter.MovementType {
			continue
		}
		// Half-open: from inclusive, to exclusive.
		if filter.OccurredFrom != nil && e.OccurredAt().Before(*filter.OccurredFrom) {
			continue
		}
		if filter.OccurredTo != nil && !e.OccurredAt().Before(*filter.OccurredTo) {
			continue
		}
		matched = append(matched, e)
	}
	sortNewestFirst(matched)
	return pagination.NewPage(paginate(matched, filter.Paging), filter.Paging, int64(len(matched))), nil
}

func sortNewestFirst(entries []*entity.InventoryLedgerEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].OccurredAt().After(entries[j].OccurredAt())
	})
}

func paginate(entries []*entity.InventoryLedgerEntry, paging pagination.Request) []*entity.InventoryLedgerEntry {
	offset, limit := paging.Offset(), paging.Limit
	if limit <= 0 || offset >= len(entries) {
		if offset >= len(entries) {
			return nil
		}
		return entries
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	return entries[offset:end]
}

// passTx runs the unit of work without a database.
type passTx struct{ calls int }

func (t *passTx) RunInTransaction(ctx context.Context, fn func(context.Context) error) error {
	t.calls++
	return fn(ctx)
}

// ---------- harness ----------

type harness struct {
	svc  *Service
	repo *fakeRepo
	tx   *passTx
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	repo := newFakeRepo()
	tx := &passTx{}
	return &harness{
		repo: repo,
		tx:   tx,
		svc:  New(repo, adapterclock.NewFakeAt("2026-08-08T10:00:00Z"), adapterid.NewSequential(), tx),
	}
}

func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zap.NewNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

type movementOpt func(*dto.RecordMovementRequest)

func atTime(ts time.Time) movementOpt {
	return func(r *dto.RecordMovementRequest) { r.OccurredAt = ts }
}
func ofType(m string) movementOpt {
	return func(r *dto.RecordMovementRequest) { r.MovementType = m }
}
func forPosition(id uuid.UUID) movementOpt {
	return func(r *dto.RecordMovementRequest) { r.PositionID = id }
}
func forProduct(id uuid.UUID) movementOpt {
	return func(r *dto.RecordMovementRequest) { r.ProductID = id }
}
func withReference(refType string, id uuid.UUID) movementOpt {
	return func(r *dto.RecordMovementRequest) { r.ReferenceType, r.ReferenceID = refType, &id }
}

func movement(opts ...movementOpt) dto.RecordMovementRequest {
	req := dto.RecordMovementRequest{
		PositionID:   uuid.New(),
		ProductID:    uuid.New(),
		WarehouseID:  uuid.New(),
		LocationID:   uuid.New(),
		MovementType: "INBOUND",
		Before:       dto.BucketSnapshotRequest{Available: 0},
		After:        dto.BucketSnapshotRequest{Available: 10},
	}
	for _, opt := range opts {
		opt(&req)
	}
	return req
}

// ---------- Append (RecordMovement) ----------

func TestRecordMovementAppendsAndDerivesDelta(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got, err := h.svc.RecordMovement(ctx, movement())
	if err != nil {
		t.Fatal(err)
	}
	if got.Delta.Available != 10 || got.Delta.OnHand != 10 {
		t.Errorf("delta = %+v, want +10", got.Delta)
	}
	if got.Before.OnHand != 0 || got.After.OnHand != 10 {
		t.Errorf("snapshots = %d -> %d", got.Before.OnHand, got.After.OnHand)
	}
	if len(h.repo.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(h.repo.entries))
	}
	// The append runs inside a transaction so it can join the inventory
	// operation's own unit of work.
	if h.tx.calls != 1 {
		t.Errorf("transaction calls = %d, want 1", h.tx.calls)
	}
}

func TestRecordMovementUsesSuppliedBusinessTime(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	backdated := time.Date(2026, 1, 15, 8, 30, 0, 0, time.UTC)

	got, err := h.svc.RecordMovement(ctx, movement(atTime(backdated)))
	if err != nil {
		t.Fatal(err)
	}
	if !got.OccurredAt.Equal(backdated) {
		t.Errorf("occurred_at = %v, want the backdated %v", got.OccurredAt, backdated)
	}

	// An unset time falls back to the injected clock, not time.Now().
	now, err := h.svc.RecordMovement(ctx, movement())
	if err != nil {
		t.Fatal(err)
	}
	if !now.OccurredAt.Equal(time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("occurred_at = %v, want the fake clock's time", now.OccurredAt)
	}
}

func TestRecordMovementRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	if _, err := h.svc.RecordMovement(ctx, movement(ofType("BOGUS"))); err == nil {
		t.Error("an unknown movement type was accepted")
	}

	negative := movement()
	negative.Before.Available = -1
	if _, err := h.svc.RecordMovement(ctx, negative); err == nil {
		t.Error("a negative before-balance was accepted")
	}

	// A reference id with no type cannot be resolved by anyone.
	orphan := movement()
	id := uuid.New()
	orphan.ReferenceID = &id
	if _, err := h.svc.RecordMovement(ctx, orphan); err == nil {
		t.Error("a reference id without a type was accepted")
	}
}

func TestRecordMovementRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.RecordMovement(context.Background(), movement()); err == nil {
		t.Fatal("recorded a movement without an authenticated principal")
	}
}

func TestRecordMovementSurfacesRepositoryFailure(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Append", errInfrastructure)

	if _, err := h.svc.RecordMovement(ctx, movement()); err == nil {
		t.Fatal("a repository failure was swallowed")
	}
	if len(h.repo.entries) != 0 {
		t.Error("an entry survived a failed append")
	}
}

// TestSubscriberSeamRecordsThroughTheSameePath proves the module can be wired
// straight into a publisher: OnInventoryMovement is the same append, so no
// adapter is needed at the composition root.
func TestSubscriberSeamRecordsThroughTheSamePath(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	var subscriber InventoryEventSubscriber = h.svc
	if err := subscriber.OnInventoryMovement(ctx, movement()); err != nil {
		t.Fatal(err)
	}
	if len(h.repo.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(h.repo.entries))
	}
}

// ---------- Immutability ----------

// TestLedgerOffersNoMutationPath is the module's defining guarantee. The
// repository contract has exactly one write method, and it only appends — there
// is no Update and no Delete to call, at any layer.
func TestLedgerOffersNoMutationPath(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	recorded, err := h.svc.RecordMovement(ctx, movement())
	if err != nil {
		t.Fatal(err)
	}

	// Re-appending the same id is refused rather than overwriting, which is what
	// makes a retried delivery safe.
	entry := h.repo.entries[0]
	if err := h.repo.Append(ctx, entry); err == nil {
		t.Fatal("appending a duplicate id overwrote history instead of failing")
	}

	after, err := h.svc.GetLedger(ctx, recorded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Delta.OnHand != recorded.Delta.OnHand || !after.OccurredAt.Equal(recorded.OccurredAt) {
		t.Fatal("the stored entry changed")
	}
	if len(h.repo.entries) != 1 {
		t.Fatalf("entries = %d, want 1 — a duplicate append added a row", len(h.repo.entries))
	}
}

// ---------- Find ----------

func TestGetLedgerIsTenantIsolated(t *testing.T) {
	h := newHarness(t)
	acme, globex := uuid.New(), uuid.New()

	recorded, err := h.svc.RecordMovement(scoped(uuid.New(), acme), movement())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.GetLedger(scoped(uuid.New(), acme), recorded.ID); err != nil {
		t.Fatalf("own-tenant read = %v", err)
	}
	// Knowing the id is not enough.
	_, err = h.svc.GetLedger(scoped(uuid.New(), globex), recorded.ID)
	if apperror.From(err).Code != apperror.CodeNotFound {
		t.Fatalf("cross-tenant read = %s, want NOT_FOUND", apperror.From(err).Code)
	}
}

func TestListByPositionReturnsOnlyThatPosition(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)
	position := uuid.New()

	for i := 0; i < 3; i++ {
		if _, err := h.svc.RecordMovement(ctx, movement(forPosition(position))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.RecordMovement(ctx, movement()); err != nil { // a different position
		t.Fatal(err)
	}

	page, err := h.svc.ListByPosition(ctx, position, pagination.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 3 {
		t.Fatalf("total = %d, want 3", page.Meta.Total)
	}
	for _, e := range page.Items {
		if e.PositionID != position {
			t.Error("the position history leaked another position")
		}
	}
}

func TestListByReferenceFindsEveryEntryForADocument(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	receipt := uuid.New()

	for i := 0; i < 2; i++ {
		if _, err := h.svc.RecordMovement(ctx, movement(withReference("PURCHASE_ORDER", receipt))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.RecordMovement(ctx, movement()); err != nil {
		t.Fatal(err)
	}

	got, err := h.svc.ListByReference(ctx, "PURCHASE_ORDER", receipt)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %d, want 2", len(got))
	}
}

// ---------- List: filtering, pagination, isolation ----------

func TestListFiltersByMovementTypeAndProduct(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	product := uuid.New()

	if _, err := h.svc.RecordMovement(ctx, movement(ofType("INBOUND"), forProduct(product))); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RecordMovement(ctx, movement(ofType("ADJUSTMENT"), forProduct(product))); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.RecordMovement(ctx, movement(ofType("INBOUND"))); err != nil {
		t.Fatal(err)
	}

	byType, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{MovementType: "ADJUSTMENT"})
	if err != nil {
		t.Fatal(err)
	}
	if byType.Meta.Total != 1 {
		t.Errorf("movement-type filter total = %d, want 1", byType.Meta.Total)
	}

	byProduct, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{ProductID: product.String()})
	if err != nil {
		t.Fatal(err)
	}
	if byProduct.Meta.Total != 2 {
		t.Errorf("product filter total = %d, want 2", byProduct.Meta.Total)
	}
}

func TestListFiltersByHalfOpenDateRange(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	jan := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	for _, ts := range []time.Time{jan, feb, mar} {
		if _, err := h.svc.RecordMovement(ctx, movement(atTime(ts))); err != nil {
			t.Fatal(err)
		}
	}

	// [Feb 1, Mar 1) must capture February only.
	page, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{
		OccurredFrom: "2026-02-01", OccurredTo: "2026-03-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 1 {
		t.Fatalf("range total = %d, want 1", page.Meta.Total)
	}
	if !page.Items[0].OccurredAt.Equal(feb) {
		t.Errorf("range returned %v, want the February entry", page.Items[0].OccurredAt)
	}

	// The upper bound is EXCLUSIVE, so an entry exactly on it is not included.
	boundary, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{
		OccurredFrom: "2026-01-10T00:00:00Z", OccurredTo: "2026-02-10T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if boundary.Meta.Total != 1 {
		t.Errorf("boundary total = %d, want 1 (from inclusive, to exclusive)", boundary.Meta.Total)
	}
}

func TestListRejectsInvalidAndInvertedDateRange(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	if _, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{OccurredFrom: "not-a-date"}); err == nil {
		t.Error("an unparseable date was accepted")
	}
	if _, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{
		OccurredFrom: "2026-03-01", OccurredTo: "2026-02-01",
	}); err == nil {
		t.Error("an inverted range was accepted")
	}
}

func TestListPaginatesNewestFirst(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := h.svc.RecordMovement(ctx, movement(atTime(base.AddDate(0, 0, i)))); err != nil {
			t.Fatal(err)
		}
	}

	var q dto.ListLedgerQuery
	q.Page, q.Limit = 1, 2
	first, err := h.svc.ListLedger(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Meta.Total != 5 || len(first.Items) != 2 {
		t.Fatalf("page 1: total=%d items=%d, want 5 and 2", first.Meta.Total, len(first.Items))
	}
	if !first.Meta.HasNext || first.Meta.TotalPages != 3 {
		t.Errorf("paging meta wrong: %+v", first.Meta)
	}
	// Newest first: the 5th of May leads.
	if !first.Items[0].OccurredAt.Equal(base.AddDate(0, 0, 4)) {
		t.Errorf("first item = %v, want the newest entry", first.Items[0].OccurredAt)
	}

	q.Page = 3
	last, err := h.svc.ListLedger(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Items) != 1 || last.Meta.HasNext {
		t.Errorf("last page: items=%d hasNext=%v, want 1 and false", len(last.Items), last.Meta.HasNext)
	}
}

func TestListIsTenantIsolated(t *testing.T) {
	h := newHarness(t)
	acme, globex := uuid.New(), uuid.New()

	for i := 0; i < 2; i++ {
		if _, err := h.svc.RecordMovement(scoped(uuid.New(), acme), movement()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.svc.RecordMovement(scoped(uuid.New(), globex), movement()); err != nil {
		t.Fatal(err)
	}

	page, err := h.svc.ListLedger(scoped(uuid.New(), acme), dto.ListLedgerQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2 (other tenant excluded)", page.Meta.Total)
	}
}

func TestListRejectsUnknownSortKey(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	var q dto.ListLedgerQuery
	q.Sort = "reason; DROP TABLE inventory_ledger_entries"
	if _, err := h.svc.ListLedger(ctx, q); err == nil {
		t.Fatal("an unallowed sort key reached the query")
	}
}

func TestListSurfacesRepositoryFailure(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("List", errInfrastructure)

	if _, err := h.svc.ListLedger(ctx, dto.ListLedgerQuery{}); err == nil {
		t.Fatal("a repository failure was swallowed")
	}
}
