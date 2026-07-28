package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
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

	deletion DeletionGuard
	zones    ZoneVerifier
}

type harnessOption func(*harness)

func withDeletionGuard(g DeletionGuard) harnessOption {
	return func(h *harness) { h.deletion = g }
}

func withZoneVerifier(v ZoneVerifier) harnessOption {
	return func(h *harness) { h.zones = v }
}

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()

	repo := newFakeRepo()

	h := &harness{
		repo:     repo,
		tx:       &fakeTxManager{repo: repo},
		events:   &fakeEventPublisher{},
		clock:    adapterclock.NewFakeAt("2026-07-25T10:00:00Z"),
		deletion: NewAlwaysAllowDeletion(),
		zones:    NewAcceptAnyZone(),
	}

	for _, opt := range opts {
		opt(h)
	}

	h.svc = New(repo, h.deletion, h.zones, h.clock,
		adapterid.NewSequential(), h.tx, h.events)

	return h
}

// scoped builds a context carrying a principal and an active company, standing
// in for auth + company middleware.
func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

// create registers a warehouse and returns it.
func (h *harness) create(t *testing.T, ctx context.Context, code, name string) dto.WarehouseResponse {
	t.Helper()

	got, err := h.svc.Create(ctx, dto.CreateWarehouseRequest{
		Code: code, Name: name, Type: "MAIN",
	})
	if err != nil {
		t.Fatalf("Create(%s) = %v", code, err)
	}
	return got
}

// makeReady brings a warehouse to the point where activation succeeds.
func (h *harness) makeReady(t *testing.T, ctx context.Context, id uuid.UUID) {
	t.Helper()

	address := "Jl. Sudirman 1"
	zoneID := uuid.New()

	if _, err := h.svc.Update(ctx, id, dto.UpdateWarehouseRequest{
		Address:         &address,
		ReceivingZoneID: &zoneID,
	}); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if _, err := h.svc.ChangeContact(ctx, id, dto.ChangeContactRequest{
		ContactName: "Budi", ContactPhone: "+62-811-1111",
	}); err != nil {
		t.Fatalf("ChangeContact() = %v", err)
	}
}

// ---------- Create ----------

func TestCreateProducesADraft(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got := h.create(t, ctx, "wh-01", "Jakarta Central")

	if got.Status != entity.StatusDraft.String() {
		t.Errorf("status = %q, want DRAFT", got.Status)
	}
	if got.Code != "WH-01" {
		t.Errorf("code = %q, want it canonicalised to WH-01", got.Code)
	}
	if got.CanReceive || got.CanShip {
		t.Error("a new warehouse reported itself operational")
	}
	if !h.events.has(entity.EventWarehouseCreated) {
		t.Errorf("WarehouseCreated not published; got %v", h.events.names())
	}
}

func TestCreateRejectsDuplicateCodeAndName(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	h.create(t, ctx, "WH-01", "Jakarta Central")

	// Different case must still collide.
	_, err := h.svc.Create(ctx, dto.CreateWarehouseRequest{
		Code: "wh-01", Name: "Somewhere Else", Type: "MAIN",
	})
	if err == nil {
		t.Fatal("Create() = nil for a duplicate code")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	// Name uniqueness matters because operators pick a destination by name.
	_, err = h.svc.Create(ctx, dto.CreateWarehouseRequest{
		Code: "WH-02", Name: "jakarta central", Type: "MAIN",
	})
	if err == nil {
		t.Fatal("Create() = nil for a duplicate name")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	tests := map[string]dto.CreateWarehouseRequest{
		"bad code":     {Code: "WH 01", Name: "Valid Name", Type: "MAIN"},
		"short name":   {Code: "WH-01", Name: "A", Type: "MAIN"},
		"unknown type": {Code: "WH-01", Name: "Valid Name", Type: "DEPOT"},
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

// TestCreateRollsBack proves the whole flow is atomic: a warehouse whose code
// is taken but which does not exist would be un-retryable.
func TestCreateRollsBack(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	h.repo.fail("Save", errInfrastructure)

	if _, err := h.svc.Create(ctx, dto.CreateWarehouseRequest{
		Code: "WH-01", Name: "Jakarta", Type: "MAIN",
	}); err == nil {
		t.Fatal("Create() = nil despite the store failing")
	}

	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.repo.count() != 0 {
		t.Error("a warehouse survived a rolled-back create")
	}
}

func TestCreateRequiresTenantAndUser(t *testing.T) {
	h := newHarness(t)

	// No context at all.
	if _, err := h.svc.Create(context.Background(), dto.CreateWarehouseRequest{
		Code: "WH-01", Name: "Jakarta", Type: "MAIN",
	}); err == nil {
		t.Error("Create() = nil with no principal")
	}

	// Authenticated but no company.
	rc := appcontext.New("test", zapNop())
	userID := uuid.New()
	rc.WithTenant(nil, &userID, "")
	noCompany := appcontext.Into(context.Background(), rc)

	_, err := h.svc.Create(noCompany, dto.CreateWarehouseRequest{
		Code: "WH-01", Name: "Jakarta", Type: "MAIN",
	})
	if err == nil {
		t.Fatal("Create() = nil with no active company")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Activation ----------

func TestActivateThroughTheService(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")

	// Incomplete: the AGGREGATE refuses, and the service simply passes it up.
	if _, err := h.svc.Activate(ctx, created.ID); err == nil {
		t.Fatal("Activate() = nil for an incomplete warehouse")
	}

	h.makeReady(t, ctx, created.ID)
	h.events.reset()

	got, err := h.svc.Activate(ctx, created.ID)
	if err != nil {
		t.Fatalf("Activate() = %v", err)
	}

	if got.Status != entity.StatusActive.String() {
		t.Errorf("status = %q, want ACTIVE", got.Status)
	}
	if !got.CanReceive || !got.CanShip {
		t.Error("an ACTIVE warehouse reported itself unable to move stock")
	}
	if !h.events.has(entity.EventWarehouseActivated) {
		t.Errorf("WarehouseActivated not published; got %v", h.events.names())
	}
}

func TestSuspendThroughTheService(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")
	h.makeReady(t, ctx, created.ID)
	_, _ = h.svc.Activate(ctx, created.ID)
	h.events.reset()

	got, err := h.svc.Suspend(ctx, created.ID,
		dto.SuspendWarehouseRequest{Reason: "failed fire inspection"})
	if err != nil {
		t.Fatalf("Suspend() = %v", err)
	}

	if got.Status != entity.StatusSuspended.String() {
		t.Errorf("status = %q, want SUSPENDED", got.Status)
	}
	if got.CanReceive || got.CanShip {
		t.Error("a SUSPENDED warehouse reported itself able to move stock")
	}

	event, ok := h.events.find(entity.EventWarehouseSuspended)
	if !ok {
		t.Fatal("WarehouseSuspended not published")
	}
	if event.Attributes["reason"] != "failed fire inspection" {
		t.Errorf("reason = %v, want it recorded", event.Attributes["reason"])
	}
}

// ---------- Archiving and the deletion extension point ----------

func TestArchiveIsSoft(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	created := h.create(t, ctx, "WH-01", "Jakarta")
	h.events.reset()

	if err := h.svc.Archive(ctx, created.ID); err != nil {
		t.Fatalf("Archive() = %v", err)
	}

	if !h.events.has(entity.EventWarehouseArchived) {
		t.Errorf("WarehouseArchived not published; got %v", h.events.names())
	}

	// The row survives — a warehouse is never hard-deleted, because future
	// stock movements will reference it forever.
	if h.repo.count() != 1 {
		t.Errorf("repository holds %d warehouses, want the archived row retained",
			h.repo.count())
	}

	got, err := h.svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() after archive = %v", err)
	}
	if !got.IsArchived || got.ArchivedAt == nil {
		t.Error("the warehouse does not report itself archived")
	}
}

// TestDeletionGuardBlocksArchive is the extension point working. It stands in
// for the Inventory sprint: a future module refuses the archive, and no
// warehouse file changed to make that possible.
func TestDeletionGuardBlocksArchive(t *testing.T) {
	guard := &blockingDeletionGuard{reason: "this warehouse still holds stock"}
	h := newHarness(t, withDeletionGuard(guard))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")

	err := h.svc.Archive(ctx, created.ID)
	if err == nil {
		t.Fatal("Archive() = nil despite the deletion guard refusing")
	}
	if guard.calls != 1 {
		t.Errorf("the guard was consulted %d times, want 1", guard.calls)
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	// And the warehouse must NOT be archived.
	got, _ := h.svc.Get(ctx, created.ID)
	if got.IsArchived {
		t.Error("the warehouse was archived despite the guard refusing")
	}
}

// TestGuardRunsBeforeTheAggregate: asking it second would mean archiving a
// warehouse and then discovering it should not have been.
func TestGuardRunsBeforeTheAggregate(t *testing.T) {
	guard := &blockingDeletionGuard{reason: "still holds stock"}
	h := newHarness(t, withDeletionGuard(guard))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")
	_ = h.svc.Archive(ctx, created.ID)
	h.events.reset()

	// A second attempt must still consult the guard rather than short-circuit
	// on an "already archived" that never happened.
	_ = h.svc.Archive(ctx, created.ID)
	if guard.calls != 2 {
		t.Errorf("the guard was consulted %d times, want 2", guard.calls)
	}
}

// ---------- Zone verification extension point ----------

func TestZoneVerifierIsConsulted(t *testing.T) {
	verifier := &countingZoneVerifier{}
	h := newHarness(t, withZoneVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")

	receiving, shipping := uuid.New(), uuid.New()
	if _, err := h.svc.Update(ctx, created.ID, dto.UpdateWarehouseRequest{
		ReceivingZoneID: &receiving,
		ShippingZoneID:  &shipping,
	}); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if verifier.calls != 2 {
		t.Errorf("the verifier was consulted %d times, want 2", verifier.calls)
	}
}

// TestZoneVerifierBlocksAssignment stands in for the Location sprint.
func TestZoneVerifierBlocksAssignment(t *testing.T) {
	verifier := &rejectingZoneVerifier{}
	h := newHarness(t, withZoneVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")

	zoneID := uuid.New()
	_, err := h.svc.Update(ctx, created.ID, dto.UpdateWarehouseRequest{
		ReceivingZoneID: &zoneID,
	})
	if err == nil {
		t.Fatal("Update() = nil despite the zone verifier refusing")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}

	got, _ := h.svc.Get(ctx, created.ID)
	if got.Zones.ReceivingZoneID != nil {
		t.Error("the zone was assigned despite the verifier refusing")
	}
}

// ---------- Cross-company isolation ----------

// twoCompanies gives each of two tenants one warehouse.
func (h *harness) twoCompanies(t *testing.T) (
	acmeCtx, globexCtx context.Context, acmeWH, globexWH dto.WarehouseResponse,
) {
	t.Helper()

	acme, globex := uuid.New(), uuid.New()
	acmeCtx = scoped(uuid.New(), acme)
	globexCtx = scoped(uuid.New(), globex)

	acmeWH = h.create(t, acmeCtx, "WH-01", "Acme Jakarta")
	globexWH = h.create(t, globexCtx, "WH-01", "Globex Jakarta")

	return acmeCtx, globexCtx, acmeWH, globexWH
}

// TestSameCodeAllowedInDifferentCompanies: uniqueness is per company, not
// global — two businesses both legitimately call their main site "WH-01".
func TestSameCodeAllowedInDifferentCompanies(t *testing.T) {
	h := newHarness(t)
	_, _, acmeWH, globexWH := h.twoCompanies(t)

	if acmeWH.ID == globexWH.ID {
		t.Fatal("the two companies share one warehouse")
	}
	if acmeWH.Code != globexWH.Code {
		t.Fatalf("test setup: codes differ (%s vs %s)", acmeWH.Code, globexWH.Code)
	}
}

func TestCannotReadAnotherCompanysWarehouse(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, _, globexWH := h.twoCompanies(t)

	_, err := h.svc.Get(acmeCtx, globexWH.ID)
	if err == nil {
		t.Fatal("cross-tenant read succeeded")
	}
	// NOT_FOUND, never FORBIDDEN — a 403 would confirm it exists.
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestListOnlyReturnsOwnWarehouses(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, acmeWH, globexWH := h.twoCompanies(t)

	page, err := h.svc.List(acmeCtx, dto.ListWarehousesQuery{})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if len(page.Items) != 1 {
		t.Fatalf("List() returned %d warehouses, want 1", len(page.Items))
	}
	if page.Items[0].ID != acmeWH.ID {
		t.Errorf("List() returned %s, want %s", page.Items[0].ID, acmeWH.ID)
	}
	for _, item := range page.Items {
		if item.ID == globexWH.ID {
			t.Error("List() leaked another tenant's warehouse")
		}
	}
}

// TestCannotMutateAnotherCompanysWarehouse covers every write path at once.
func TestCannotMutateAnotherCompanysWarehouse(t *testing.T) {
	h := newHarness(t)
	acmeCtx, _, _, globexWH := h.twoCompanies(t)

	name := "Hijacked"
	operations := map[string]error{
		"Update": func() error {
			_, err := h.svc.Update(acmeCtx, globexWH.ID,
				dto.UpdateWarehouseRequest{Name: &name})
			return err
		}(),
		"Activate": func() error {
			_, err := h.svc.Activate(acmeCtx, globexWH.ID)
			return err
		}(),
		"Suspend": func() error {
			_, err := h.svc.Suspend(acmeCtx, globexWH.ID,
				dto.SuspendWarehouseRequest{Reason: "hostile"})
			return err
		}(),
		"ChangeContact": func() error {
			_, err := h.svc.ChangeContact(acmeCtx, globexWH.ID,
				dto.ChangeContactRequest{ContactName: "X", ContactPhone: "+1"})
			return err
		}(),
		"Archive": h.svc.Archive(acmeCtx, globexWH.ID),
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

// ---------- Rename ----------

// TestRenameToOwnNameIsNotAConflict covers the ExistsByNameExcluding path:
// without the exclusion, renaming a warehouse to its current name would fail.
func TestRenameToOwnNameIsNotAConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta Central")

	same := "Jakarta Central"
	if _, err := h.svc.Update(ctx, created.ID,
		dto.UpdateWarehouseRequest{Name: &same}); err != nil {
		t.Errorf("renaming to the same name = %v, want nil", err)
	}
}

func TestRenameToAnotherWarehousesNameIsAConflict(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID)

	h.create(t, ctx, "WH-01", "Jakarta Central")
	second := h.create(t, ctx, "WH-02", "Surabaya")

	taken := "Jakarta Central"
	_, err := h.svc.Update(ctx, second.ID, dto.UpdateWarehouseRequest{Name: &taken})
	if err == nil {
		t.Fatal("renaming onto a taken name succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// ---------- Events ----------

// TestServicePublishesOnlyWhatTheAggregateRecorded is the DDD property: the
// service never constructs an event, so an event exists exactly when a
// transition happened.
func TestServicePublishesOnlyWhatTheAggregateRecorded(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	created := h.create(t, ctx, "WH-01", "Jakarta")
	h.events.reset()

	// A description change raises nothing in the aggregate.
	description := "Updated description"
	if _, err := h.svc.Update(ctx, created.ID,
		dto.UpdateWarehouseRequest{Description: &description}); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if got := h.events.names(); len(got) != 0 {
		t.Errorf("a description change published %v, want nothing", got)
	}
}

func TestEventsCarryTenantAndActor(t *testing.T) {
	h := newHarness(t)
	companyID, actorID := uuid.New(), uuid.New()
	ctx := scoped(actorID, companyID)

	created := h.create(t, ctx, "WH-01", "Jakarta")

	event, ok := h.events.find(entity.EventWarehouseCreated)
	if !ok {
		t.Fatal("WarehouseCreated was not published")
	}
	if event.CompanyID != companyID {
		t.Errorf("event company = %s, want %s", event.CompanyID, companyID)
	}
	if event.ActorID != actorID {
		t.Errorf("event actor = %s, want %s", event.ActorID, actorID)
	}
	if event.WarehouseID != created.ID {
		t.Errorf("event warehouse = %s, want %s", event.WarehouseID, created.ID)
	}
}
