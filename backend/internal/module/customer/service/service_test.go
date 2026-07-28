package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
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
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	repo := newFakeRepo()
	h := &harness{repo: repo, tx: &fakeTxManager{repo: repo}, events: &fakeEventPublisher{}}
	h.svc = New(repo, adapterclock.NewFakeAt("2026-08-01T10:00:00Z"), adapterid.NewSequential(), h.tx, h.events)
	return h
}

func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

func (h *harness) create(t *testing.T, ctx context.Context, code, name string) dto.CustomerResponse {
	t.Helper()
	got, err := h.svc.Create(ctx, dto.CreateCustomerRequest{Code: code, Name: name})
	if err != nil {
		t.Fatalf("Create(%s) = %v", code, err)
	}
	return got
}

// ---------- Create ----------

func TestCreateProducesActiveCustomer(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got, err := h.svc.Create(ctx, dto.CreateCustomerRequest{
		Code: "cus-01", Name: "Acme Retail", Email: "sales@acme.test", City: "Jakarta",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", got.Status)
	}
	if got.Code != "CUS-01" {
		t.Errorf("code = %q, want canonicalised CUS-01", got.Code)
	}
	if got.Email != "sales@acme.test" || got.City != "Jakarta" {
		t.Errorf("contact/address not mapped: %+v", got)
	}
	if !h.events.has(entity.EventCustomerCreated) {
		t.Error("no CustomerCreated event")
	}
}

func TestCreateRejectsDuplicateCode(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.create(t, ctx, "CUS-1", "First")

	_, err := h.svc.Create(ctx, dto.CreateCustomerRequest{Code: "cus-1", Name: "Second"})
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestCreateRejectsInvalidEmail(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	_, err := h.svc.Create(ctx, dto.CreateCustomerRequest{Code: "CUS-1", Name: "Acme", Email: "not-an-email"})
	if err == nil {
		t.Fatal("an invalid email was accepted")
	}
}

func TestCreateRollsBackOnRepoFailure(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Save", errInfrastructure)

	_, err := h.svc.Create(ctx, dto.CreateCustomerRequest{Code: "CUS-1", Name: "Acme"})
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
	_, err := h.svc.Create(context.Background(), dto.CreateCustomerRequest{Code: "CUS-1", Name: "Acme"})
	if err == nil {
		t.Fatal("create succeeded without an authenticated principal")
	}
}

func TestSameCodeAllowedAcrossCompanies(t *testing.T) {
	h := newHarness(t)
	h.create(t, scoped(uuid.New(), uuid.New()), "CUS-1", "Acme")
	h.create(t, scoped(uuid.New(), uuid.New()), "CUS-1", "Globex")
}

// ---------- Update ----------

func TestUpdateReplacesAttributes(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	c := h.create(t, ctx, "CUS-1", "Acme")
	h.events.reset()

	got, err := h.svc.Update(ctx, c.ID, dto.UpdateCustomerRequest{Name: "Acme Retail International", Phone: "+62-811"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Acme Retail International" || got.Phone != "+62-811" {
		t.Errorf("update not applied: %+v", got)
	}
	if got.Code != "CUS-1" {
		t.Error("update changed the code")
	}
	if !h.events.has(entity.EventCustomerUpdated) {
		t.Error("no CustomerUpdated event")
	}
}

func TestUpdateRejectsBlankName(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	c := h.create(t, ctx, "CUS-1", "Acme")
	_, err := h.svc.Update(ctx, c.ID, dto.UpdateCustomerRequest{Name: " "})
	if err == nil {
		t.Fatal("blank name accepted")
	}
}

// ---------- Lifecycle ----------

func TestActivateDeactivate(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	c := h.create(t, ctx, "CUS-1", "Acme")

	off, err := h.svc.Deactivate(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if off.Status != "INACTIVE" {
		t.Errorf("status = %q, want INACTIVE", off.Status)
	}
	if !h.events.has(entity.EventCustomerDeactivated) {
		t.Error("no CustomerDeactivated event")
	}

	on, err := h.svc.Activate(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if on.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", on.Status)
	}
}

// ---------- Tenant isolation + concurrency ----------

func TestGetIsTenantIsolated(t *testing.T) {
	h := newHarness(t)
	c := h.create(t, scoped(uuid.New(), uuid.New()), "CUS-1", "Acme")

	_, err := h.svc.Get(scoped(uuid.New(), uuid.New()), c.ID)
	if apperror.From(err).Code != apperror.CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", apperror.From(err).Code)
	}
}

func TestConcurrentModificationBecomesConflict(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	c := h.create(t, ctx, "CUS-1", "Acme")

	h.repo.fail("Update", sharedrepo.ErrConcurrentModification)
	_, err := h.svc.Deactivate(ctx, c.ID)
	appErr := apperror.From(err)
	if appErr.Code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", appErr.Code)
	}
	if appErr.Details == nil {
		t.Error("conflict should carry the current version")
	}
}

func TestListIsTenantScoped(t *testing.T) {
	h := newHarness(t)
	acme, globex := uuid.New(), uuid.New()
	h.create(t, scoped(uuid.New(), acme), "CUS-1", "Acme One")
	h.create(t, scoped(uuid.New(), acme), "CUS-2", "Acme Two")
	h.create(t, scoped(uuid.New(), globex), "CUS-1", "Globex One")

	page, err := h.svc.List(scoped(uuid.New(), acme), dto.ListCustomersQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
}
