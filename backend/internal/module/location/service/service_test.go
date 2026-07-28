package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/location/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type harness struct {
	svc    *Service
	repo   *fakeRepo
	tx     *fakeTxManager
	events *fakeEventPublisher
	clock  *adapterclock.Fake

	deps Dependencies
}

type option func(*Dependencies)

func withWarehouses(v WarehouseVerifier) option {
	return func(d *Dependencies) { d.Warehouses = v }
}

func withCapacity(p CurrentCapacityProvider) option {
	return func(d *Dependencies) { d.Capacity = p }
}

func withInventory(p InventoryProvider) option {
	return func(d *Dependencies) { d.Inventory = p }
}

func withReceiving(g ReceivingGuard) option {
	return func(d *Dependencies) { d.Receiving = g }
}

func withPicking(g PickingGuard) option {
	return func(d *Dependencies) { d.Picking = g }
}

func withCounting(g CycleCountGuard) option {
	return func(d *Dependencies) { d.Counting = g }
}

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()

	repo := newFakeRepo()
	tx := &fakeTxManager{repo: repo}
	events := &fakeEventPublisher{}
	clock := adapterclock.NewFakeAt("2026-07-26T10:00:00Z")

	deps := Dependencies{
		Repo:       repo,
		Warehouses: NewAcceptAnyWarehouse(),
		Capacity:   NewEmptyCapacity(),
		Inventory:  NewEmptyInventory(),
		Receiving:  NewAllowAllReceiving(),
		Picking:    NewAllowAllPicking(),
		Counting:   NewAllowAllCycleCount(),
		Clock:      clock,
		IDs:        adapterid.NewSequential(),
		Tx:         tx,
		Events:     events,
	}

	for _, opt := range opts {
		opt(&deps)
	}

	return &harness{
		svc:    New(deps),
		repo:   repo,
		tx:     tx,
		events: events,
		clock:  clock,
		deps:   deps,
	}
}

// scoped builds a context carrying a principal and an active company, standing
// in for auth + company middleware.
func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

func (h *harness) create(
	t *testing.T, ctx context.Context, warehouseID uuid.UUID, zone, aisle string,
) dto.LocationResponse {
	t.Helper()

	got, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: warehouseID, Zone: zone, Aisle: aisle,
	})
	if err != nil {
		t.Fatalf("Create(%s-%s) = %v", zone, aisle, err)
	}
	return got
}

// ---------- Create ----------

func TestCreateProducesActiveLocation(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got := h.create(t, ctx, uuid.New(), "a", "01")

	if got.Status != entity.StatusActive.String() {
		t.Errorf("status = %q, want ACTIVE", got.Status)
	}
	if got.Code != "A-01" {
		t.Errorf("code = %q, want it derived from the coordinate", got.Code)
	}
	if !got.CanReceive || !got.CanPick {
		t.Error("a new location reported itself unusable")
	}
	if !got.Capacity.IsUnlimited {
		t.Error("a new location has capacity limits it was never given")
	}
	if !h.events.has(entity.EventLocationCreated) {
		t.Errorf("LocationCreated not published; got %v", h.events.names())
	}
}

// TestCreateVerifiesTheWarehouse proves the cross-aggregate reference is
// checked rather than assumed.
func TestCreateVerifiesTheWarehouse(t *testing.T) {
	verifier := &countingWarehouseVerifier{}
	h := newHarness(t, withWarehouses(verifier))
	ctx := scoped(uuid.New(), uuid.New())

	h.create(t, ctx, uuid.New(), "A", "01")

	if verifier.calls != 1 {
		t.Errorf("the warehouse verifier was consulted %d times, want 1", verifier.calls)
	}
}

// TestCreateRejectedByWarehouseVerifier stands in for a warehouse that does not
// exist, or belongs to another tenant.
func TestCreateRejectedByWarehouseVerifier(t *testing.T) {
	verifier := &rejectingWarehouseVerifier{}
	h := newHarness(t, withWarehouses(verifier))
	ctx := scoped(uuid.New(), uuid.New())

	_, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "A", Aisle: "01",
	})
	if err == nil {
		t.Fatal("Create() = nil despite the warehouse verifier refusing")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
	if h.repo.count() != 0 {
		t.Error("a location was created in a nonexistent warehouse")
	}
}

func TestCreateRejectsDuplicateCodeInSameWarehouse(t *testing.T) {
	h := newHarness(t)
	companyID, warehouseID := uuid.New(), uuid.New()
	ctx := scoped(uuid.New(), companyID)

	h.create(t, ctx, warehouseID, "A", "01")

	_, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: warehouseID, Zone: "a", Aisle: "01",
	})
	if err == nil {
		t.Fatal("Create() = nil for a duplicate code")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestSameCodeAllowedInDifferentWarehouses: aisle numbering restarts at every
// building, so "A-01" in two sites is normal.
func TestSameCodeAllowedInDifferentWarehouses(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	first := h.create(t, ctx, uuid.New(), "A", "01")
	second := h.create(t, ctx, uuid.New(), "A", "01")

	if first.Code != second.Code {
		t.Fatalf("test setup: codes differ (%s vs %s)", first.Code, second.Code)
	}
	if first.ID == second.ID {
		t.Error("the two warehouses share one location")
	}
}

// TestCreateRejectsDuplicateBarcodeAcrossWarehouses: barcode uniqueness is
// per COMPANY, because a scanner reads a label with no idea which site it is
// standing in.
func TestCreateRejectsDuplicateBarcodeAcrossWarehouses(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	if _, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "A", Aisle: "01", Barcode: "LOC-000123",
	}); err != nil {
		t.Fatalf("first Create() = %v", err)
	}

	// A DIFFERENT warehouse, same company.
	_, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "B", Aisle: "01", Barcode: "LOC-000123",
	})
	if err == nil {
		t.Fatal("Create() = nil for a barcode already used in another warehouse")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	tests := map[string]dto.CreateLocationRequest{
		"no zone":         {WarehouseID: uuid.New(), Zone: ""},
		"gapped":          {WarehouseID: uuid.New(), Zone: "A", Rack: "02"},
		"bad barcode":     {WarehouseID: uuid.New(), Zone: "A", Barcode: "ab"},
		"bad capacity":    {WarehouseID: uuid.New(), Zone: "A", MaxWeight: "heavy"},
		"negative weight": {WarehouseID: uuid.New(), Zone: "A", MaxWeight: "-5"},
	}

	for label, req := range tests {
		t.Run(label, func(t *testing.T) {
			if _, err := h.svc.Create(ctx, req); err == nil {
				t.Fatal("Create() = nil, want a validation error")
			} else if code := apperror.From(err).Code; code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", code)
			}
		})
	}
}

func TestCreateRollsBack(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	h.repo.fail("Save", errors.New("database unreachable"))

	if _, err := h.svc.Create(ctx, dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "A", Aisle: "01",
	}); err == nil {
		t.Fatal("Create() = nil despite the store failing")
	}

	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.repo.count() != 0 {
		t.Error("a location survived a rolled-back create")
	}
}

func TestCreateRequiresTenantAndUser(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.Create(context.Background(), dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "A",
	}); err == nil {
		t.Error("Create() = nil with no principal")
	}

	rc := appcontext.New("test", zapNop())
	userID := uuid.New()
	rc.WithTenant(nil, &userID, "")
	noCompany := appcontext.Into(context.Background(), rc)

	_, err := h.svc.Create(noCompany, dto.CreateLocationRequest{
		WarehouseID: uuid.New(), Zone: "A",
	})
	if err == nil {
		t.Fatal("Create() = nil with no active company")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Capacity: the CurrentCapacityProvider extension point ----------

// TestChangeCapacityConsultsTheProvider proves the service fetches the fact the
// aggregate cannot see.
func TestChangeCapacityConsultsTheProvider(t *testing.T) {
	provider := &stubCapacity{}
	h := newHarness(t, withCapacity(provider))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	if _, err := h.svc.ChangeCapacity(ctx, created.ID, dto.ChangeCapacityRequest{
		MaxWeight: "500",
	}); err != nil {
		t.Fatalf("ChangeCapacity() = %v", err)
	}

	if provider.calls != 1 {
		t.Errorf("the capacity provider was consulted %d times, want 1", provider.calls)
	}
}

// TestCapacityBlockedByCurrentUsage is the extension point working end to end:
// a future module reports stock, and the aggregate refuses the reduction — with
// no location file changing.
func TestCapacityBlockedByCurrentUsage(t *testing.T) {
	weight, err := entity.NewQuantity("400", "usage")
	if err != nil {
		t.Fatalf("NewQuantity() = %v", err)
	}

	provider := &stubCapacity{usage: entity.Usage{Weight: weight}}
	h := newHarness(t, withCapacity(provider))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	// 500 accommodates 400.
	if _, err := h.svc.ChangeCapacity(ctx, created.ID, dto.ChangeCapacityRequest{
		MaxWeight: "500",
	}); err != nil {
		t.Fatalf("ChangeCapacity(500) = %v", err)
	}

	// 300 does not.
	_, err = h.svc.ChangeCapacity(ctx, created.ID, dto.ChangeCapacityRequest{
		MaxWeight: "300",
	})
	if err == nil {
		t.Fatal("capacity was reduced below the reported usage")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	got, _ := h.svc.Get(ctx, created.ID)
	if got.Capacity.MaxWeight != "500.000" {
		t.Errorf("max_weight = %q, want it unchanged at 500", got.Capacity.MaxWeight)
	}
}

func TestCapacityChangeRaisesEvent(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")
	h.events.reset()

	if _, err := h.svc.ChangeCapacity(ctx, created.ID, dto.ChangeCapacityRequest{
		MaxWeight: "1000", MaxVolume: "2.5",
	}); err != nil {
		t.Fatalf("ChangeCapacity() = %v", err)
	}

	if !h.events.has(entity.EventCapacityChanged) {
		t.Errorf("CapacityChanged not published; got %v", h.events.names())
	}
}

// ---------- Mixed SKU: the InventoryProvider extension point ----------

// TestDisableMixedSKUConsultsInventory: disabling NARROWS a rule, so the
// aggregate needs to know what is stored. Enabling widens and needs no fact.
func TestDisableMixedSKUConsultsInventory(t *testing.T) {
	provider := &stubInventory{distinctSKUs: 0, empty: true}
	h := newHarness(t, withInventory(provider))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	enable := true
	if _, err := h.svc.Update(ctx, created.ID, dto.UpdateLocationRequest{
		AllowMixedSKU: &enable,
	}); err != nil {
		t.Fatalf("enabling = %v", err)
	}
	if provider.skuCalls != 0 {
		t.Errorf("the inventory provider was consulted %d times on ENABLE, want 0",
			provider.skuCalls)
	}

	disable := false
	if _, err := h.svc.Update(ctx, created.ID, dto.UpdateLocationRequest{
		AllowMixedSKU: &disable,
	}); err != nil {
		t.Fatalf("disabling = %v", err)
	}
	if provider.skuCalls != 1 {
		t.Errorf("the inventory provider was consulted %d times on DISABLE, want 1",
			provider.skuCalls)
	}
}

func TestDisableMixedSKUBlockedByStoredSKUs(t *testing.T) {
	provider := &stubInventory{distinctSKUs: 3, empty: false}
	h := newHarness(t, withInventory(provider))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	enable := true
	_, _ = h.svc.Update(ctx, created.ID, dto.UpdateLocationRequest{AllowMixedSKU: &enable})

	disable := false
	_, err := h.svc.Update(ctx, created.ID, dto.UpdateLocationRequest{
		AllowMixedSKU: &disable,
	})
	if err == nil {
		t.Fatal("mixed SKUs were disabled while three SKUs are stored")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// ---------- Archiving: the InventoryProvider extension point ----------

func TestArchiveIsSoft(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	if err := h.svc.Archive(ctx, created.ID); err != nil {
		t.Fatalf("Archive() = %v", err)
	}

	// The row survives — a location is never hard-deleted, because future stock
	// movements will reference it forever.
	if h.repo.count() != 1 {
		t.Errorf("repository holds %d locations, want the archived row retained",
			h.repo.count())
	}

	got, err := h.svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after archive = %v", err)
	}
	if !got.IsArchived || got.ArchivedAt == nil {
		t.Error("the location does not report itself archived")
	}
}

// TestArchiveBlockedByStoredStock stands in for the Inventory sprint.
func TestArchiveBlockedByStoredStock(t *testing.T) {
	provider := &stubInventory{empty: false}
	h := newHarness(t, withInventory(provider))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	err := h.svc.Archive(ctx, created.ID)
	if err == nil {
		t.Fatal("Archive() = nil despite the location holding stock")
	}
	if provider.emptyCalls != 1 {
		t.Errorf("the inventory provider was consulted %d times, want 1",
			provider.emptyCalls)
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	got, _ := h.svc.Get(ctx, created.ID)
	if got.IsArchived {
		t.Error("the location was archived despite holding stock")
	}
}

// ---------- Availability composition ----------

// TestCanReceiveComposesAggregateAndGuard: the aggregate answers the LOCAL
// question, the guard the cross-aggregate one, and neither can be skipped.
func TestCanReceiveComposesAggregateAndGuard(t *testing.T) {
	guard := &blockingReceivingGuard{}
	h := newHarness(t, withReceiving(guard))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	// The location is ACTIVE, so the aggregate permits — but the guard refuses.
	err := h.svc.CanReceive(ctx, created.ID)
	if err == nil {
		t.Fatal("CanReceive() = nil despite the guard refusing")
	}
	if guard.calls != 1 {
		t.Errorf("the guard was consulted %d times, want 1", guard.calls)
	}

	// Lock it: now the AGGREGATE refuses first, and the guard is not reached —
	// there is no point asking a downstream module about a location nobody may
	// touch.
	guard.calls = 0
	if _, err := h.svc.Lock(ctx, created.ID, dto.LockLocationRequest{
		Reason: "damaged racking",
	}); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	if err := h.svc.CanReceive(ctx, created.ID); err == nil {
		t.Fatal("CanReceive() = nil for a LOCKED location")
	}
	if guard.calls != 0 {
		t.Errorf("the guard was consulted for a LOCKED location %d times, want 0",
			guard.calls)
	}
}

func TestCanPickUsesItsOwnGuard(t *testing.T) {
	guard := &blockingPickingGuard{}
	h := newHarness(t, withPicking(guard))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	if err := h.svc.CanPick(ctx, created.ID); err == nil {
		t.Fatal("CanPick() = nil despite the guard refusing")
	}
	if guard.calls != 1 {
		t.Errorf("the picking guard was consulted %d times, want 1", guard.calls)
	}
}

// TestMaintenanceAllowsPickingNotReceiving is the asymmetry, verified through
// the service rather than only on the aggregate.
func TestMaintenanceAllowsPickingNotReceiving(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	if _, err := h.svc.StartMaintenance(ctx, created.ID); err != nil {
		t.Fatalf("StartMaintenance() = %v", err)
	}

	if err := h.svc.CanReceive(ctx, created.ID); err == nil {
		t.Error("a MAINTENANCE location accepted receiving")
	}
	if err := h.svc.CanPick(ctx, created.ID); err != nil {
		t.Errorf("CanPick() on MAINTENANCE = %v; its stock would be stranded", err)
	}
}

// TestCanCountIgnoresAvailability: a location is often LOCKED *in order to*
// count it, so applying the receiving rules would refuse exactly when a count
// is most needed.
func TestCanCountIgnoresAvailability(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")
	if _, err := h.svc.Lock(ctx, created.ID, dto.LockLocationRequest{
		Reason: "stock discrepancy",
	}); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	if err := h.svc.CanCount(ctx, created.ID); err != nil {
		t.Errorf("CanCount() on a LOCKED location = %v, want nil", err)
	}
}

func TestCanCountUsesItsOwnGuard(t *testing.T) {
	guard := &blockingCycleCountGuard{}
	h := newHarness(t, withCounting(guard))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	if err := h.svc.CanCount(ctx, created.ID); err == nil {
		t.Fatal("CanCount() = nil despite the guard refusing")
	}
	if guard.calls != 1 {
		t.Errorf("the cycle-count guard was consulted %d times, want 1", guard.calls)
	}
}

// ---------- Lock / Unlock ----------

func TestLockAndUnlockRaiseEvents(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")
	h.events.reset()

	locked, err := h.svc.Lock(ctx, created.ID, dto.LockLocationRequest{
		Reason: "damaged racking",
	})
	if err != nil {
		t.Fatalf("Lock() = %v", err)
	}
	if locked.Status != entity.StatusLocked.String() {
		t.Errorf("status = %q, want LOCKED", locked.Status)
	}
	if locked.CanReceive || locked.CanPick {
		t.Error("a LOCKED location reported itself usable")
	}

	event, ok := h.events.find(entity.EventLocationLocked)
	if !ok {
		t.Fatal("LocationLocked was not published")
	}
	if event.Attributes["reason"] != "damaged racking" {
		t.Errorf("reason = %v, want it recorded", event.Attributes["reason"])
	}

	unlocked, err := h.svc.Unlock(ctx, created.ID)
	if err != nil {
		t.Fatalf("Unlock() = %v", err)
	}
	if unlocked.Status != entity.StatusActive.String() {
		t.Errorf("status = %q, want ACTIVE", unlocked.Status)
	}
	if !h.events.has(entity.EventLocationUnlocked) {
		t.Errorf("LocationUnlocked not published; got %v", h.events.names())
	}
}

// ---------- Barcode ----------

func TestAssignBarcodeAndLookup(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	created := h.create(t, ctx, uuid.New(), "A", "01")
	h.events.reset()

	if _, err := h.svc.AssignBarcode(ctx, created.ID, dto.AssignBarcodeRequest{
		Barcode: "LOC-000123",
	}); err != nil {
		t.Fatalf("AssignBarcode() = %v", err)
	}
	if !h.events.has(entity.EventBarcodeAssigned) {
		t.Errorf("BarcodeAssigned not published; got %v", h.events.names())
	}

	// The scanner path: resolve by barcode with no warehouse supplied.
	got, err := h.svc.GetByBarcode(ctx, "LOC-000123")
	if err != nil {
		t.Fatalf("GetByBarcode() = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("resolved to %s, want %s", got.ID, created.ID)
	}
}

// TestReassigningOwnBarcodeIsNotAConflict covers the ExistsByBarcodeExcluding
// path: without the exclusion, re-sending a location its current barcode would
// fail.
func TestReassigningOwnBarcodeIsNotAConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")

	for i := 0; i < 2; i++ {
		if _, err := h.svc.AssignBarcode(ctx, created.ID, dto.AssignBarcodeRequest{
			Barcode: "LOC-000123",
		}); err != nil {
			t.Fatalf("AssignBarcode() call %d = %v", i+1, err)
		}
	}
}

func TestAssignTakenBarcodeIsAConflict(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	first := h.create(t, ctx, uuid.New(), "A", "01")
	second := h.create(t, ctx, uuid.New(), "B", "01")

	if _, err := h.svc.AssignBarcode(ctx, first.ID, dto.AssignBarcodeRequest{
		Barcode: "LOC-000123",
	}); err != nil {
		t.Fatalf("first AssignBarcode() = %v", err)
	}

	_, err := h.svc.AssignBarcode(ctx, second.ID, dto.AssignBarcodeRequest{
		Barcode: "LOC-000123",
	})
	if err == nil {
		t.Fatal("assigning a taken barcode succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// ---------- Cross-company isolation ----------

func (h *harness) twoCompanies(t *testing.T) (
	acmeCtx, globexCtx context.Context, acmeLoc, globexLoc dto.LocationResponse,
) {
	t.Helper()

	acme, globex := uuid.New(), uuid.New()
	acmeCtx = scoped(uuid.New(), acme)
	globexCtx = scoped(uuid.New(), globex)

	acmeLoc = h.create(t, acmeCtx, uuid.New(), "A", "01")
	globexLoc = h.create(t, globexCtx, uuid.New(), "A", "01")

	return acmeCtx, globexCtx, acmeLoc, globexLoc
}

func TestCannotReadAnotherCompanysLocation(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, _, globexLoc := h.twoCompanies(t)

	_, err := h.svc.Get(acmeCtx, globexLoc.ID)
	if err == nil {
		t.Fatal("cross-tenant read succeeded")
	}
	// NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestListOnlyReturnsOwnLocations(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, acmeLoc, globexLoc := h.twoCompanies(t)

	page, err := h.svc.List(acmeCtx, dto.ListLocationsQuery{})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if len(page.Items) != 1 || page.Items[0].ID != acmeLoc.ID {
		t.Fatalf("List() returned %+v, want only Acme's location", page.Items)
	}
	for _, item := range page.Items {
		if item.ID == globexLoc.ID {
			t.Error("List() leaked another tenant's location")
		}
	}
}

func TestBarcodeLookupIsIsolated(t *testing.T) {
	h := newHarness(t)
	acmeCtx, globexCtx, _, globexLoc := h.twoCompanies(t)

	if _, err := h.svc.AssignBarcode(globexCtx, globexLoc.ID, dto.AssignBarcodeRequest{
		Barcode: "LOC-000123",
	}); err != nil {
		t.Fatalf("AssignBarcode() = %v", err)
	}

	// Acme scans the same label: it must not resolve.
	if _, err := h.svc.GetByBarcode(acmeCtx, "LOC-000123"); err == nil {
		t.Error("a barcode resolved across tenants")
	}
}

func TestCannotMutateAnotherCompanysLocation(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, _, globexLoc := h.twoCompanies(t)

	priority := 5
	operations := map[string]error{
		"Update": func() error {
			_, err := h.svc.Update(acmeCtx, globexLoc.ID,
				dto.UpdateLocationRequest{PickingPriority: &priority})
			return err
		}(),
		"Lock": func() error {
			_, err := h.svc.Lock(acmeCtx, globexLoc.ID,
				dto.LockLocationRequest{Reason: "hostile"})
			return err
		}(),
		"ChangeCapacity": func() error {
			_, err := h.svc.ChangeCapacity(acmeCtx, globexLoc.ID,
				dto.ChangeCapacityRequest{MaxWeight: "1"})
			return err
		}(),
		"AssignBarcode": func() error {
			_, err := h.svc.AssignBarcode(acmeCtx, globexLoc.ID,
				dto.AssignBarcodeRequest{Barcode: "LOC-999999"})
			return err
		}(),
		"Deactivate": func() error {
			_, err := h.svc.Deactivate(acmeCtx, globexLoc.ID)
			return err
		}(),
		"Archive": h.svc.Archive(acmeCtx, globexLoc.ID),
	}

	for name, err := range operations {
		if err == nil {
			t.Errorf("%s succeeded across tenants", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeNotFound {
			t.Errorf("%s: code = %s, want NOT_FOUND", name, code)
		}
	}
}

// ---------- Events ----------

// TestServicePublishesOnlyWhatTheAggregateRecorded is the DDD property: the
// service never constructs an event, so an event exists exactly when a
// transition happened.
func TestServicePublishesOnlyWhatTheAggregateRecorded(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, uuid.New(), "A", "01")
	h.events.reset()

	// A priority change raises nothing in the aggregate.
	priority := 5
	if _, err := h.svc.Update(ctx, created.ID, dto.UpdateLocationRequest{
		PickingPriority: &priority,
	}); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if got := h.events.names(); len(got) != 0 {
		t.Errorf("a priority change published %v, want nothing", got)
	}
}

func TestEventsCarryWarehouseTenantAndActor(t *testing.T) {
	h := newHarness(t)
	companyID, actorID, warehouseID := uuid.New(), uuid.New(), uuid.New()
	ctx := scoped(actorID, companyID)

	created := h.create(t, ctx, warehouseID, "A", "01")

	event, ok := h.events.find(entity.EventLocationCreated)
	if !ok {
		t.Fatal("LocationCreated was not published")
	}
	if event.CompanyID != companyID {
		t.Errorf("event company = %s, want %s", event.CompanyID, companyID)
	}
	if event.WarehouseID != warehouseID {
		t.Errorf("event warehouse = %s, want %s", event.WarehouseID, warehouseID)
	}
	if event.ActorID != actorID {
		t.Errorf("event actor = %s, want %s", event.ActorID, actorID)
	}
	if event.LocationID != created.ID {
		t.Errorf("event location = %s, want %s", event.LocationID, created.ID)
	}
}
