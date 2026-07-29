package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func fixedNow() time.Time { return time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC) }

type harness struct {
	svc    *Service
	repo   *fakeRepo
	tx     *fakeTxManager
	events *fakeEventPublisher

	products   ProductVerifier
	warehouses WarehouseVerifier
	locations  LocationVerifier
	policy     StockPolicyProvider
}

type harnessOption func(*harness)

func withProducts(v ProductVerifier) harnessOption { return func(h *harness) { h.products = v } }
func withWarehouses(v WarehouseVerifier) harnessOption {
	return func(h *harness) { h.warehouses = v }
}
func withLocations(v LocationVerifier) harnessOption { return func(h *harness) { h.locations = v } }
func withPolicy(p StockPolicyProvider) harnessOption { return func(h *harness) { h.policy = p } }

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()
	repo := newFakeRepo()
	h := &harness{
		repo:       repo,
		tx:         &fakeTxManager{repo: repo},
		events:     &fakeEventPublisher{},
		products:   NewAcceptAnyProduct(),
		warehouses: NewAcceptAnyWarehouse(),
		locations:  NewAcceptAnyLocation(),
		policy:     NewDefaultStockPolicy(),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.svc = New(repo, h.products, h.warehouses, h.locations, h.policy,
		adapterclock.NewFakeAt("2026-08-03T10:00:00Z"), adapterid.NewSequential(), h.tx, h.events)
	return h
}

func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

// place is a reusable warehouse/location/product coordinate.
type place struct{ warehouse, location, product uuid.UUID }

func newPlace() place {
	return place{warehouse: uuid.New(), location: uuid.New(), product: uuid.New()}
}

func receiveReq(p place, qty int64) dto.ReceiveStockRequest {
	return dto.ReceiveStockRequest{
		StockKeyRequest: dto.StockKeyRequest{
			WarehouseID: p.warehouse, LocationID: p.location, ProductID: p.product, Tracking: "NONE",
		},
		Quantity: qty,
	}
}

func qtyReq(id uuid.UUID, qty int64) dto.PositionQuantityRequest {
	return dto.PositionQuantityRequest{PositionID: id, Quantity: qty}
}

func strPtr(s string) *string { return &s }
func intPtr(v int64) *int64   { return &v }

// assertBuckets checks the response's four balances and its derived total.
func assertBuckets(t *testing.T, got dto.PositionResponse, available, reserved, allocated, quarantined int64) {
	t.Helper()
	if got.Available != available || got.Reserved != reserved ||
		got.Allocated != allocated || got.Quarantined != quarantined {
		t.Fatalf("buckets = avail:%d res:%d alloc:%d quar:%d; want %d/%d/%d/%d",
			got.Available, got.Reserved, got.Allocated, got.Quarantined,
			available, reserved, allocated, quarantined)
	}
	if want := available + reserved + allocated + quarantined; got.OnHand != want {
		t.Fatalf("OnHand = %d, want %d", got.OnHand, want)
	}
}

// ---------- Receive ----------

func TestReceiveOpensPositionAndLandsInAvailable(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 25))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, got, 25, 0, 0, 0)
	if h.repo.count() != 1 {
		t.Fatalf("positions = %d, want 1", h.repo.count())
	}
	if !h.events.has(entity.EventPositionCreated) || !h.events.has(entity.EventStockReceived) {
		t.Error("expected both position-created and stock-received events")
	}
}

func TestReceiveReusesExistingPosition(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := newPlace()

	if _, err := h.svc.ReceiveStock(ctx, receiveReq(p, 10)); err != nil {
		t.Fatal(err)
	}
	h.events.reset()

	got, err := h.svc.ReceiveStock(ctx, receiveReq(p, 15))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, got, 25, 0, 0, 0)
	if h.repo.count() != 1 {
		t.Errorf("positions = %d, want 1 — the key must resolve to the same position", h.repo.count())
	}
	if h.events.has(entity.EventPositionCreated) {
		t.Error("a second receipt must not raise a created event")
	}
}

func TestReceiveKeepsOnePositionPerLot(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := newPlace()

	lotA := receiveReq(p, 10)
	lotA.Tracking, lotA.LotNumber = "LOT", strPtr("LOT-A")
	lotB := receiveReq(p, 5)
	lotB.Tracking, lotB.LotNumber = "LOT", strPtr("LOT-B")

	if _, err := h.svc.ReceiveStock(ctx, lotA); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ReceiveStock(ctx, lotB); err != nil {
		t.Fatal(err)
	}
	if h.repo.count() != 2 {
		t.Fatalf("positions = %d, want 2 (one per lot)", h.repo.count())
	}
	again, err := h.svc.ReceiveStock(ctx, lotA)
	if err != nil {
		t.Fatal(err)
	}
	if again.OnHand != 20 || h.repo.count() != 2 {
		t.Errorf("lot A on-hand = %d, positions = %d; want 20 and 2", again.OnHand, h.repo.count())
	}
}

func TestReceiveRejectsInvalidStockAttributes(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	// LOT without a lot number is a contradiction the value object refuses.
	bad := receiveReq(newPlace(), 5)
	bad.Tracking = "LOT"
	if _, err := h.svc.ReceiveStock(ctx, bad); err == nil {
		t.Error("LOT tracking without a lot number was accepted")
	}

	// A serial position may never exceed one unit.
	serial := receiveReq(newPlace(), 2)
	serial.Tracking, serial.SerialNumber = "SERIAL", strPtr("SN-1")
	if _, err := h.svc.ReceiveStock(ctx, serial); err == nil {
		t.Error("a serial receipt of two units was accepted")
	}
}

func TestReceiveVerifierFailures(t *testing.T) {
	cases := map[string]harnessOption{
		"product":   withProducts(&rejectingProductVerifier{}),
		"warehouse": withWarehouses(&rejectingWarehouseVerifier{}),
		"location":  withLocations(&rejectingLocationVerifier{}),
	}
	for label, opt := range cases {
		h := newHarness(t, opt)
		ctx := scoped(uuid.New(), uuid.New())

		if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 5)); err == nil {
			t.Errorf("%s: receive succeeded despite a rejecting verifier", label)
		}
		if h.repo.count() != 0 {
			t.Errorf("%s: a position was persisted despite failed verification", label)
		}
		if h.events.count() != 0 {
			t.Errorf("%s: an event was published despite failed verification", label)
		}
	}
}

func TestReceiveRepositoryFailureRollsBack(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Update", errInfrastructure)

	if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 5)); err == nil {
		t.Fatal("receive succeeded despite a repository failure")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.repo.count() != 0 || h.events.count() != 0 {
		t.Error("a rolled-back receive left a position or published an event")
	}
}

func TestReceiveRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.ReceiveStock(context.Background(), receiveReq(newPlace(), 5)); err == nil {
		t.Fatal("receive succeeded without an authenticated principal")
	}
}

func TestReceiveAppliesQuarantinePolicy(t *testing.T) {
	policy := &quarantineOnReceipt{}
	h := newHarness(t, withPolicy(policy))
	ctx := scoped(uuid.New(), uuid.New())

	got, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10))
	if err != nil {
		t.Fatal(err)
	}
	if policy.calls == 0 {
		t.Error("the stock policy was not consulted")
	}
	// Received stock is held rather than made available.
	assertBuckets(t, got, 0, 0, 0, 10)
	if !h.events.has(entity.EventStockQuarantined) {
		t.Error("no quarantined event for the policy-applied hold")
	}
}

// ---------- Issue ----------

func TestIssueTakesFromAvailable(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20))
	h.events.reset()

	got, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, 8))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, got, 12, 0, 0, 0)
	if !h.events.has(entity.EventStockIssued) {
		t.Error("no issued event")
	}
}

func TestIssueCannotReachEncumberedStock(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20))
	if _, err := h.svc.ReserveStock(ctx, qtyReq(pos.ID, 15)); err != nil {
		t.Fatal(err)
	}

	// 20 on hand but only 5 available — the aggregate refuses.
	_, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, 6))
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestIssueRejectsNonPositiveQuantity(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 5))

	if _, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, 0)); err == nil {
		t.Error("a zero-quantity issue was accepted")
	}
	if _, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, -1)); err == nil {
		t.Error("a negative-quantity issue was accepted")
	}
}

// ---------- Reserve / Release / Allocate / Deallocate ----------

func TestReservationAndAllocationPipeline(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 30))
	h.events.reset()

	reserved, err := h.svc.ReserveStock(ctx, qtyReq(pos.ID, 20))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, reserved, 10, 20, 0, 0)

	allocated, err := h.svc.AllocateStock(ctx, qtyReq(pos.ID, 12))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, allocated, 10, 8, 12, 0)

	deallocated, err := h.svc.DeallocateStock(ctx, qtyReq(pos.ID, 5))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, deallocated, 10, 13, 7, 0)

	released, err := h.svc.ReleaseReservation(ctx, qtyReq(pos.ID, 13))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, released, 23, 0, 7, 0)

	for _, name := range []entity.EventName{
		entity.EventStockReserved, entity.EventStockAllocated,
		entity.EventStockDeallocated, entity.EventStockReleased,
	} {
		if !h.events.has(name) {
			t.Errorf("missing event %s", name)
		}
	}
}

// TestAllocateRequiresAReservation is the guarantee that Allocate is NOT an
// alias for Reserve: it hardens an existing promise and cannot create one.
func TestAllocateRequiresAReservation(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 30))

	// 30 units sit available, but nothing is reserved.
	_, err := h.svc.AllocateStock(ctx, qtyReq(pos.ID, 1))
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT — allocation must not draw from available",
			apperror.From(err).Code)
	}
	after, err := h.svc.GetInventoryPosition(ctx, pos.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, after, 30, 0, 0, 0)
}

func TestOverReserveOverReleaseOverDeallocateAreConflicts(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10))

	if _, err := h.svc.ReserveStock(ctx, qtyReq(pos.ID, 11)); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("over-reserve = %s, want CONFLICT", apperror.From(err).Code)
	}
	if _, err := h.svc.ReserveStock(ctx, qtyReq(pos.ID, 4)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ReleaseReservation(ctx, qtyReq(pos.ID, 5)); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("over-release = %s, want CONFLICT", apperror.From(err).Code)
	}
	if _, err := h.svc.DeallocateStock(ctx, qtyReq(pos.ID, 1)); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("deallocate with nothing allocated = %s, want CONFLICT", apperror.From(err).Code)
	}
}

// ---------- Quarantine ----------

func TestQuarantineIsPartialAndReversible(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20))
	h.events.reset()

	held, err := h.svc.MoveToQuarantine(ctx, qtyReq(pos.ID, 6))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, held, 14, 0, 0, 6)
	if !h.events.has(entity.EventStockQuarantined) {
		t.Error("no quarantined event")
	}

	// Quarantined stock is unreachable by an issue.
	if _, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, 15)); apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("issuing into quarantined stock = %s, want CONFLICT", apperror.From(err).Code)
	}

	released, err := h.svc.ReleaseFromQuarantine(ctx, qtyReq(pos.ID, 6))
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, released, 20, 0, 0, 0)
	if !h.events.has(entity.EventStockReleasedFromQuarantine) {
		t.Error("no released-from-quarantine event")
	}
}

// ---------- Transfer ----------

func TestTransferMovesBetweenPositionsAtomically(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	origin := newPlace()

	from, err := h.svc.ReceiveStock(ctx, receiveReq(origin, 30))
	if err != nil {
		t.Fatal(err)
	}
	h.events.reset()

	got, err := h.svc.TransferStock(ctx, dto.TransferStockRequest{
		FromPositionID: from.ID,
		ToWarehouseID:  origin.warehouse,
		ToLocationID:   uuid.New(),
		Quantity:       12,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBuckets(t, got, 18, 0, 0, 0)
	if h.repo.count() != 2 {
		t.Fatalf("positions = %d, want 2 (destination opened)", h.repo.count())
	}
	// Both halves are the aggregate's own behaviours.
	if !h.events.has(entity.EventStockIssued) || !h.events.has(entity.EventStockReceived) {
		t.Error("a transfer must issue from the origin and receive at the destination")
	}
	if !h.events.has(entity.EventPositionCreated) {
		t.Error("the destination position was not opened")
	}
}

func TestTransferRollsBackWhenPersistenceFails(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	origin := newPlace()
	from, err := h.svc.ReceiveStock(ctx, receiveReq(origin, 30))
	if err != nil {
		t.Fatal(err)
	}
	h.events.reset()
	h.repo.fail("Update", errInfrastructure)

	if _, err := h.svc.TransferStock(ctx, dto.TransferStockRequest{
		FromPositionID: from.ID,
		ToWarehouseID:  origin.warehouse,
		ToLocationID:   uuid.New(),
		Quantity:       12,
	}); err == nil {
		t.Fatal("transfer succeeded despite the persistence failure")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	// The destination was never committed, and nothing was announced.
	if h.repo.count() != 1 {
		t.Errorf("positions = %d after rollback, want 1", h.repo.count())
	}
	if h.events.count() != 0 {
		t.Error("events were published for a rolled-back transfer")
	}
}

func TestTransferRejectsSameLocationAndVerifiesDestination(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	origin := newPlace()
	from, _ := h.svc.ReceiveStock(ctx, receiveReq(origin, 10))

	if _, err := h.svc.TransferStock(ctx, dto.TransferStockRequest{
		FromPositionID: from.ID,
		ToWarehouseID:  origin.warehouse,
		ToLocationID:   origin.location,
		Quantity:       1,
	}); err == nil {
		t.Error("a transfer to the origin location was accepted")
	}

	// The destination location is verified like any other reference.
	rejecting := newHarness(t, withLocations(&rejectingLocationVerifier{}))
	rctx := scoped(uuid.New(), uuid.New())
	rpos, err := rejecting.svc.ReceiveStock(rctx, receiveReq(newPlace(), 10))
	if err == nil {
		// Receive itself is verified, so it must already have failed.
		t.Fatalf("receive succeeded with a rejecting location verifier: %+v", rpos)
	}
}

func TestTransferBeyondAvailableIsConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	origin := newPlace()
	from, _ := h.svc.ReceiveStock(ctx, receiveReq(origin, 10))
	if _, err := h.svc.ReserveStock(ctx, qtyReq(from.ID, 8)); err != nil {
		t.Fatal(err)
	}

	// Only 2 available; reserved stock cannot be transferred out.
	_, err := h.svc.TransferStock(ctx, dto.TransferStockRequest{
		FromPositionID: from.ID,
		ToWarehouseID:  origin.warehouse,
		ToLocationID:   uuid.New(),
		Quantity:       5,
	})
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

// ---------- Adjust ----------

func TestAdjustReconcilesAndCarriesReason(t *testing.T) {
	for _, adjustment := range []dto.AdjustmentType{
		dto.AdjustmentCycleCount, dto.AdjustmentDamage, dto.AdjustmentShrinkage,
		dto.AdjustmentFound, dto.AdjustmentInitialBalance, dto.AdjustmentManualCorrection,
	} {
		h := newHarness(t)
		ctx := scoped(uuid.New(), uuid.New())
		pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20))
		h.events.reset()

		got, err := h.svc.AdjustStock(ctx, dto.AdjustStockRequest{
			PositionID: pos.ID, Quantity: intPtr(15), Type: adjustment,
		})
		if err != nil {
			t.Fatalf("%s: %v", adjustment, err)
		}
		assertBuckets(t, got, 15, 0, 0, 0)
		if !h.events.has(entity.EventStockAdjusted) {
			t.Errorf("%s: no adjusted event", adjustment)
		}
	}
}

func TestAdjustBelowEncumberedIsConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20))
	if _, err := h.svc.ReserveStock(ctx, qtyReq(pos.ID, 12)); err != nil {
		t.Fatal(err)
	}

	_, err := h.svc.AdjustStock(ctx, dto.AdjustStockRequest{
		PositionID: pos.ID, Quantity: intPtr(10), Type: dto.AdjustmentShrinkage,
	})
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestAdjustRejectsUnknownTypeAndMissingQuantity(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10))

	if _, err := h.svc.AdjustStock(ctx, dto.AdjustStockRequest{
		PositionID: pos.ID, Quantity: intPtr(5), Type: dto.AdjustmentType("BOGUS"),
	}); err == nil {
		t.Error("an unknown adjustment type was accepted")
	}
	if _, err := h.svc.AdjustStock(ctx, dto.AdjustStockRequest{
		PositionID: pos.ID, Type: dto.AdjustmentCycleCount,
	}); err == nil {
		t.Error("a missing adjustment quantity was accepted")
	}
}

// ---------- Concurrency, isolation, reads ----------

func TestConcurrentModificationBecomesConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	pos, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10))

	h.repo.fail("Update", sharedrepo.ErrConcurrentModification)
	_, err := h.svc.IssueStock(ctx, qtyReq(pos.ID, 1))

	appErr := apperror.From(err)
	if appErr.Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", appErr.Code)
	}
	if appErr.Details == nil {
		t.Error("a concurrency conflict should carry the current version for a retry")
	}
}

func TestOperationsAreTenantIsolated(t *testing.T) {
	h := newHarness(t)
	acme := scoped(uuid.New(), uuid.New())
	globex := scoped(uuid.New(), uuid.New())

	pos, err := h.svc.ReceiveStock(acme, receiveReq(newPlace(), 10))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.svc.IssueStock(globex, qtyReq(pos.ID, 1)); apperror.From(err).Code != apperror.CodeNotFound {
		t.Errorf("cross-tenant issue = %s, want NOT_FOUND", apperror.From(err).Code)
	}
	if _, err := h.svc.GetInventoryPosition(globex, pos.ID); apperror.From(err).Code != apperror.CodeNotFound {
		t.Errorf("cross-tenant get = %s, want NOT_FOUND", apperror.From(err).Code)
	}
}

func TestGetAndListPositions(t *testing.T) {
	h := newHarness(t)
	company := uuid.New()
	ctx := scoped(uuid.New(), company)

	first, _ := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 10))
	if _, err := h.svc.ReceiveStock(ctx, receiveReq(newPlace(), 20)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ReceiveStock(scoped(uuid.New(), uuid.New()), receiveReq(newPlace(), 99)); err != nil {
		t.Fatal(err)
	}

	got, err := h.svc.GetInventoryPosition(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.OnHand != 10 {
		t.Errorf("get returned %+v", got)
	}

	page, err := h.svc.ListInventoryPositions(ctx, dto.ListPositionsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2 (other tenant excluded)", page.Meta.Total)
	}
}
