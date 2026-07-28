package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/port"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type harness struct {
	svc    *Service
	repo   *fakeRepo
	tx     *fakeTxManager
	events *fakeEventPublisher

	products     port.ProductProvider
	warehouses   port.WarehouseProvider
	locations    port.LocationProvider
	reservations port.ReservationProvider
}

type harnessOption func(*harness)

func withProducts(p port.ProductProvider) harnessOption { return func(h *harness) { h.products = p } }
func withWarehouses(p port.WarehouseProvider) harnessOption {
	return func(h *harness) { h.warehouses = p }
}
func withLocations(p port.LocationProvider) harnessOption {
	return func(h *harness) { h.locations = p }
}
func withReservations(p port.ReservationProvider) harnessOption {
	return func(h *harness) { h.reservations = p }
}

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	repo := newFakeRepo()
	h := &harness{
		repo:         repo,
		tx:           &fakeTxManager{repo: repo},
		events:       &fakeEventPublisher{},
		products:     port.NewAcceptAnyProduct(),
		warehouses:   port.NewAcceptAnyWarehouse(),
		locations:    port.NewAcceptAnyLocation(),
		reservations: port.NewNoReservations(),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.svc = New(repo, h.products, h.warehouses, h.locations, h.reservations,
		adapterclock.NewFakeAt("2026-07-30T10:00:00Z"), adapterid.NewSequential(), h.tx, h.events)
	return h
}

func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

// createNone opens a NONE position and returns the response.
func (h *harness) createNone(t *testing.T, ctx context.Context, qty int64) dto.InventoryResponse {
	t.Helper()
	got, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
		WarehouseID: uuid.New(), LocationID: uuid.New(), ProductID: uuid.New(),
		Tracking: "NONE", Quantity: qty,
	})
	if err != nil {
		t.Fatalf("CreateInventory(NONE) = %v", err)
	}
	return got
}

func i64(v int64) *int64 { return &v }

// ---------- Create ----------

func TestCreateNoneSucceedsAndPublishes(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got := h.createNone(t, ctx, 25)
	if got.Status != "ACTIVE" || got.Tracking != "NONE" || got.OnHand != 25 || got.Available != 25 {
		t.Errorf("unexpected response: %+v", got)
	}
	if !h.events.has(entity.EventInventoryCreated) {
		t.Error("no InventoryCreated event published")
	}
}

func TestCreateRejectsDuplicateNone(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	req := dto.CreateInventoryRequest{
		WarehouseID: uuid.New(), LocationID: uuid.New(), ProductID: uuid.New(), Tracking: "NONE", Quantity: 5,
	}
	if _, err := h.svc.CreateInventory(ctx, req); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.CreateInventory(ctx, req) // same product+location
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestCreateLotAndSerial(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	wh, loc, prod := uuid.New(), uuid.New(), uuid.New()
	lot := "LOT-A"

	if _, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
		WarehouseID: wh, LocationID: loc, ProductID: prod, Tracking: "LOT", LotNumber: &lot, Quantity: 100,
	}); err != nil {
		t.Fatalf("create LOT = %v", err)
	}
	// Duplicate lot in the same place conflicts.
	if _, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
		WarehouseID: wh, LocationID: loc, ProductID: prod, Tracking: "LOT", LotNumber: &lot, Quantity: 5,
	}); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("duplicate lot code = %s, want CONFLICT", apperror.From(err).Code)
	}

	serial := "SN-1"
	got, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
		WarehouseID: wh, LocationID: uuid.New(), ProductID: prod, Tracking: "SERIAL", SerialNumber: &serial, Quantity: 1,
	})
	if err != nil {
		t.Fatalf("create SERIAL = %v", err)
	}
	if got.SerialNumber == nil || *got.SerialNumber != "SN-1" || got.OnHand != 1 {
		t.Errorf("serial response wrong: %+v", got)
	}
}

func TestCreateVerifiesEveryReference(t *testing.T) {
	cases := map[string]harnessOption{
		"product":   withProducts(&rejectingProductProvider{}),
		"warehouse": withWarehouses(&rejectingWarehouseProvider{}),
		"location":  withLocations(&rejectingLocationProvider{}),
	}
	for label, opt := range cases {
		h := newHarness(t, opt)
		ctx := scoped(uuid.New(), uuid.New())
		_, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
			WarehouseID: uuid.New(), LocationID: uuid.New(), ProductID: uuid.New(), Tracking: "NONE", Quantity: 1,
		})
		if err == nil {
			t.Errorf("%s: create succeeded despite a rejecting provider", label)
		}
		if h.repo.count() != 0 {
			t.Errorf("%s: a record was saved despite failed verification", label)
		}
	}
}

func TestCreateRollsBackOnRepoFailure(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Save", errInfrastructure)

	_, err := h.svc.CreateInventory(ctx, dto.CreateInventoryRequest{
		WarehouseID: uuid.New(), LocationID: uuid.New(), ProductID: uuid.New(), Tracking: "NONE", Quantity: 1,
	})
	if err == nil {
		t.Fatal("create succeeded despite repo failure")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.events.count() != 0 {
		t.Error("an event was published for a rolled-back create")
	}
}

func TestCreateRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.CreateInventory(context.Background(), dto.CreateInventoryRequest{
		WarehouseID: uuid.New(), LocationID: uuid.New(), ProductID: uuid.New(), Tracking: "NONE",
	})
	if err == nil {
		t.Fatal("create succeeded without an authenticated principal")
	}
}

// ---------- Quantity operations ----------

func TestIncreaseAndDecrease(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)
	h.events.reset()

	up, err := h.svc.IncreaseInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 5})
	if err != nil {
		t.Fatal(err)
	}
	if up.OnHand != 15 || !h.events.has(entity.EventInventoryIncreased) {
		t.Errorf("increase wrong: on=%d", up.OnHand)
	}

	down, err := h.svc.DecreaseInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 6})
	if err != nil {
		t.Fatal(err)
	}
	if down.OnHand != 9 || !h.events.has(entity.EventInventoryDecreased) {
		t.Errorf("decrease wrong: on=%d", down.OnHand)
	}
}

func TestReserveAndRelease(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)

	res, err := h.svc.ReserveInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 7})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reserved != 7 || res.Available != 3 {
		t.Errorf("reserve wrong: res=%d avl=%d", res.Reserved, res.Available)
	}
	rel, err := h.svc.ReleaseReservation(ctx, inv.ID, dto.QuantityRequest{Quantity: 4})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Reserved != 3 || rel.Available != 7 {
		t.Errorf("release wrong: res=%d avl=%d", rel.Reserved, rel.Available)
	}
}

func TestReserveBeyondAvailableIsAggregateConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 5)
	// The aggregate — not the service — enforces this.
	_, err := h.svc.ReserveInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 6})
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestAdjustAndCycleCount(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)
	h.events.reset()

	adj, err := h.svc.AdjustInventory(ctx, inv.ID, dto.AdjustInventoryRequest{Quantity: i64(8), Reason: "damage"})
	if err != nil {
		t.Fatal(err)
	}
	if adj.OnHand != 8 || !h.events.has(entity.EventInventoryAdjusted) {
		t.Errorf("adjust wrong: on=%d", adj.OnHand)
	}

	cc, err := h.svc.CompleteCycleCount(ctx, inv.ID, dto.CycleCountRequest{Counted: i64(6)})
	if err != nil {
		t.Fatal(err)
	}
	if cc.OnHand != 6 || !h.events.has(entity.EventCycleCountCompleted) {
		t.Errorf("cycle count wrong: on=%d", cc.OnHand)
	}
}

func TestTransfers(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)
	h.events.reset()

	out, err := h.svc.TransferOut(ctx, inv.ID, dto.QuantityRequest{Quantity: 4})
	if err != nil {
		t.Fatal(err)
	}
	if out.OnHand != 6 || !h.events.has(entity.EventInventoryTransferred) {
		t.Errorf("transfer out wrong: on=%d", out.OnHand)
	}
	in, err := h.svc.TransferIn(ctx, inv.ID, dto.QuantityRequest{Quantity: 10})
	if err != nil {
		t.Fatal(err)
	}
	if in.OnHand != 16 {
		t.Errorf("transfer in wrong: on=%d", in.OnHand)
	}
}

func TestDecreaseBlockedByReservationProvider(t *testing.T) {
	provider := &blockingReservationProvider{}
	h := newHarness(t, withReservations(provider))
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)

	// The cross-aggregate guard blocks removal, without touching the aggregate.
	_, err := h.svc.DecreaseInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 1})
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
	if provider.calls == 0 {
		t.Error("the reservation provider was not consulted")
	}
}

// ---------- Lifecycle ----------

func TestLockFreezesMovements(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)

	locked, err := h.svc.LockInventory(ctx, inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Status != "LOCKED" {
		t.Errorf("status = %q, want LOCKED", locked.Status)
	}
	// A movement on a locked record is refused by the aggregate.
	if _, err := h.svc.IncreaseInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 1}); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("increase-while-locked code = %s, want CONFLICT", apperror.From(err).Code)
	}
	unlocked, err := h.svc.UnlockInventory(ctx, inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unlocked.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", unlocked.Status)
	}
}

// ---------- Tenant isolation + concurrency ----------

func TestGetIsTenantIsolated(t *testing.T) {
	h := newHarness(t)
	inv := h.createNone(t, scoped(uuid.New(), uuid.New()), 5)

	_, err := h.svc.GetInventory(scoped(uuid.New(), uuid.New()), inv.ID)
	if apperror.From(err).Code != apperror.CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", apperror.From(err).Code)
	}
}

func TestConcurrentModificationBecomesConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	inv := h.createNone(t, ctx, 10)

	// Simulate the repository losing the optimistic-lock race on the next Save.
	h.repo.fail("Save", sharedrepo.ErrConcurrentModification)
	_, err := h.svc.IncreaseInventory(ctx, inv.ID, dto.QuantityRequest{Quantity: 1})
	appErr := apperror.From(err)
	if appErr.Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", appErr.Code)
	}
	if appErr.Details == nil {
		t.Error("conflict should carry the current version for a retry")
	}
}

func TestListIsTenantScoped(t *testing.T) {
	h := newHarness(t)
	acme, globex := uuid.New(), uuid.New()
	h.createNone(t, scoped(uuid.New(), acme), 1)
	h.createNone(t, scoped(uuid.New(), acme), 2)
	h.createNone(t, scoped(uuid.New(), globex), 3)

	page, err := h.svc.ListInventory(scoped(uuid.New(), acme), dto.ListInventoriesQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2 (globex excluded)", page.Meta.Total)
	}
}
