package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
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

	categories CategoryVerifier
	brands     BrandVerifier
	uoms       UOMVerifier
	inventory  InventoryProvider
}

type harnessOption func(*harness)

func withUOMVerifier(v UOMVerifier) harnessOption {
	return func(h *harness) { h.uoms = v }
}
func withCategoryVerifier(v CategoryVerifier) harnessOption {
	return func(h *harness) { h.categories = v }
}
func withInventory(p InventoryProvider) harnessOption {
	return func(h *harness) { h.inventory = p }
}

func newHarness(t *testing.T, opts ...harnessOption) *harness {
	t.Helper()

	repo := newFakeRepo()
	h := &harness{
		repo:       repo,
		tx:         &fakeTxManager{repo: repo},
		events:     &fakeEventPublisher{},
		categories: NewAcceptAnyCategory(),
		brands:     NewAcceptAnyBrand(),
		uoms:       NewAcceptAnyUOM(),
		inventory:  NewNoInventory(),
	}
	for _, opt := range opts {
		opt(h)
	}

	h.svc = New(repo, h.categories, h.brands, h.uoms, h.inventory,
		adapterclock.NewFakeAt("2026-07-28T10:00:00Z"), adapterid.NewSequential(), h.tx, h.events)

	return h
}

// scoped builds a context carrying a principal and an active company, standing
// in for the auth + company middleware.
func scoped(userID, companyID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), "OWNER")
	return appcontext.Into(context.Background(), rc)
}

// create registers a product and returns it.
func (h *harness) create(t *testing.T, ctx context.Context, sku, name string) dto.ProductResponse {
	t.Helper()
	got, err := h.svc.Create(ctx, dto.CreateProductRequest{
		SKU: sku, Name: name, BaseUOMID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("Create(%s) = %v", sku, err)
	}
	return got
}

func strptr(s string) *string { return &s }

// ---------- Create ----------

func TestCreateProducesADraft(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	got := h.create(t, ctx, "sku-1", "Blue Widget")

	if got.Status != entity.StatusDraft.String() {
		t.Errorf("status = %q, want DRAFT", got.Status)
	}
	if got.Tracking != entity.TrackingNone.String() {
		t.Errorf("tracking = %q, want NONE", got.Tracking)
	}
	if got.SKU != "SKU-1" {
		t.Errorf("sku = %q, want it canonicalised to SKU-1", got.SKU)
	}
	// Base unit provisioned with factor 1.
	if len(got.UOMs) != 1 || !got.UOMs[0].IsBase || got.UOMs[0].Factor != "1" {
		t.Errorf("base unit not provisioned: %+v", got.UOMs)
	}
	if !h.events.has(entity.EventProductCreated) {
		t.Error("no ProductCreated event published")
	}
}

func TestCreateRejectsDuplicateSKU(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.create(t, ctx, "SKU-1", "First")

	// Different case must still collide — the index is CITEXT.
	_, err := h.svc.Create(ctx, dto.CreateProductRequest{SKU: "sku-1", Name: "Second", BaseUOMID: uuid.New()})
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", code)
	}
}

func TestCreateRejectsDuplicateName(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.create(t, ctx, "SKU-1", "Blue Widget")

	_, err := h.svc.Create(ctx, dto.CreateProductRequest{SKU: "SKU-2", Name: "blue widget", BaseUOMID: uuid.New()})
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", code)
	}
}

func TestCreateVerifiesBaseUnit(t *testing.T) {
	verifier := &rejectingUOMVerifier{}
	h := newHarness(t, withUOMVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())

	_, err := h.svc.Create(ctx, dto.CreateProductRequest{SKU: "SKU-1", Name: "Widget", BaseUOMID: uuid.New()})
	if err == nil {
		t.Fatal("a product with an unknown base unit was accepted")
	}
	if verifier.calls == 0 {
		t.Error("the service did not consult the UOM verifier")
	}
	if h.repo.count() != 0 {
		t.Error("a product survived a failed base-unit verification")
	}
}

func TestCreateRollsBackOnRepoFailure(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.repo.fail("Save", errInfrastructure)

	_, err := h.svc.Create(ctx, dto.CreateProductRequest{SKU: "SKU-1", Name: "Widget", BaseUOMID: uuid.New()})
	if err == nil {
		t.Fatal("Create() = nil despite the repository failing")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.events.has(entity.EventProductCreated) {
		t.Error("an event was published for a rolled-back create")
	}
}

func TestCreateRequiresAuthentication(t *testing.T) {
	h := newHarness(t)
	// No principal in context.
	_, err := h.svc.Create(context.Background(), dto.CreateProductRequest{SKU: "SKU-1", Name: "Widget", BaseUOMID: uuid.New()})
	if err == nil {
		t.Fatal("Create() succeeded without an authenticated principal")
	}
}

// ---------- Tenant isolation ----------

func TestGetIsTenantIsolated(t *testing.T) {
	h := newHarness(t)
	acme := uuid.New()
	globex := uuid.New()

	created := h.create(t, scoped(uuid.New(), acme), "SKU-1", "Acme Widget")

	// Knowing the id is not enough: scoped to another company it does not exist.
	_, err := h.svc.Get(scoped(uuid.New(), globex), created.ID)
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", code)
	}
}

func TestSameSKUAllowedAcrossCompanies(t *testing.T) {
	h := newHarness(t)
	h.create(t, scoped(uuid.New(), uuid.New()), "SKU-1", "Widget A")
	// A different company reusing the SKU is fine — uniqueness is per tenant.
	h.create(t, scoped(uuid.New(), uuid.New()), "SKU-1", "Widget B")
}

// ---------- Rename specification ----------

func TestRenameToOwnNameIsAllowed(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	// Renaming to the same name must not trip UniqueProductName against itself.
	if _, err := h.svc.Update(ctx, p.ID, dto.UpdateProductRequest{Name: strptr("Widget")}); err != nil {
		t.Fatalf("Update() = %v", err)
	}
}

func TestRenameToAnotherProductsNameConflicts(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	h.create(t, ctx, "SKU-1", "Widget One")
	p2 := h.create(t, ctx, "SKU-2", "Widget Two")

	_, err := h.svc.Update(ctx, p2.ID, dto.UpdateProductRequest{Name: strptr("Widget One")})
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT", code)
	}
}

// ---------- Barcode specification ----------

func TestAddBarcodeRejectsCrossProductDuplicate(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p1 := h.create(t, ctx, "SKU-1", "Widget One")
	p2 := h.create(t, ctx, "SKU-2", "Widget Two")

	if _, err := h.svc.AddBarcode(ctx, p1.ID, dto.AddBarcodeRequest{Barcode: "8991234567890", Primary: true}); err != nil {
		t.Fatalf("AddBarcode() = %v", err)
	}
	_, err := h.svc.AddBarcode(ctx, p2.ID, dto.AddBarcodeRequest{Barcode: "8991234567890"})
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT for a barcode owned by another product", code)
	}
}

func TestFirstBarcodeBecomesPrimary(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	got, err := h.svc.AddBarcode(ctx, p.ID, dto.AddBarcodeRequest{Barcode: "A", Primary: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Barcodes) != 1 || !got.Barcodes[0].IsPrimary {
		t.Errorf("first barcode should be primary: %+v", got.Barcodes)
	}
	if !h.events.has(entity.EventBarcodeAdded) {
		t.Error("no BarcodeAdded event")
	}
}

// ---------- UOM verifier ----------

func TestAddUOMVerifiesTheUnit(t *testing.T) {
	verifier := &armableUOMVerifier{}
	h := newHarness(t, withUOMVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget") // base-unit check succeeds
	verifier.reject = true                   // now reject the alternate unit
	verifier.calls = 0

	_, err := h.svc.AddUOM(ctx, p.ID, dto.AddUOMRequest{UOMID: uuid.New(), Factor: "12"})
	if err == nil {
		t.Fatal("an alternate unit that does not exist was accepted")
	}
	if verifier.calls == 0 {
		t.Error("the service did not consult the UOM verifier on AddUOM")
	}
}

func TestAddUOMStoresExactFactor(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	got, err := h.svc.AddUOM(ctx, p.ID, dto.AddUOMRequest{UOMID: uuid.New(), Factor: "0.333"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range got.UOMs {
		if u.Factor == "333/1000" {
			found = true
		}
	}
	if !found {
		t.Errorf("factor not stored as an exact rational: %+v", got.UOMs)
	}
}

// ---------- Category verifier ----------

func TestAssignCategoryVerifiesExistence(t *testing.T) {
	verifier := &rejectingCategoryVerifier{}
	h := newHarness(t, withCategoryVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	id := uuid.New()
	_, err := h.svc.AssignCategory(ctx, p.ID, dto.AssignCategoryRequest{CategoryID: &id})
	if err == nil {
		t.Fatal("an unknown category was accepted")
	}
	if verifier.calls == 0 {
		t.Error("the service did not consult the category verifier")
	}
}

func TestClearCategorySkipsVerification(t *testing.T) {
	verifier := &rejectingCategoryVerifier{}
	h := newHarness(t, withCategoryVerifier(verifier))
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	// Clearing (nil id) must not consult the verifier — there is nothing to
	// verify — so a rejecting verifier does not block it.
	if _, err := h.svc.AssignCategory(ctx, p.ID, dto.AssignCategoryRequest{CategoryID: nil}); err != nil {
		t.Fatalf("clearing category = %v", err)
	}
	if verifier.calls != 0 {
		t.Error("clearing a category consulted the verifier")
	}
}

// ---------- Tracking + inventory ----------

func TestSetTrackingBlockedWhenInventoryExists(t *testing.T) {
	h := newHarness(t, withInventory(&fakeInventoryProvider{has: true}))
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	_, err := h.svc.SetTracking(ctx, p.ID, dto.SetTrackingRequest{Method: "LOT"})
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Fatalf("code = %s, want CONFLICT while stock exists", code)
	}
}

func TestSetTrackingSucceedsWithoutInventory(t *testing.T) {
	h := newHarness(t) // NoInventory default
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	got, err := h.svc.SetTracking(ctx, p.ID, dto.SetTrackingRequest{Method: "SERIAL"})
	if err != nil {
		t.Fatalf("SetTracking() = %v", err)
	}
	if got.Tracking != "SERIAL" {
		t.Errorf("tracking = %q, want SERIAL", got.Tracking)
	}
	if !h.events.has(entity.EventTrackingMethodChanged) {
		t.Error("no TrackingMethodChanged event")
	}
}

// ---------- Measurements ----------

func TestSetMeasurementsRoundTripsExactStrings(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	got, err := h.svc.SetMeasurements(ctx, p.ID, dto.SetMeasurementsRequest{
		Weight:    strptr("2.5"),
		Volume:    strptr("0.001"),
		Dimension: &dto.DimensionInput{Width: "10", Height: "20", Length: "30"},
	})
	if err != nil {
		t.Fatalf("SetMeasurements() = %v", err)
	}
	if got.Weight == nil || *got.Weight != "5/2" {
		t.Errorf("weight = %v, want exact 5/2", got.Weight)
	}
	if got.Dimension == nil || got.Dimension.Width != "10" {
		t.Errorf("dimension not stored: %+v", got.Dimension)
	}
}

func TestSetMeasurementsRejectsInvalidNumber(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	_, err := h.svc.SetMeasurements(ctx, p.ID, dto.SetMeasurementsRequest{Weight: strptr("NaN")})
	if err == nil {
		t.Fatal("a non-finite weight was accepted")
	}
}

// ---------- Shelf life ----------

func TestSetShelfLifeDefinedVsCleared(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	zero := 0
	got, err := h.svc.SetShelfLife(ctx, p.ID, dto.SetShelfLifeRequest{Days: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ShelfLifeDefined || got.ShelfLifeDays == nil || *got.ShelfLifeDays != 0 {
		t.Errorf("a zero-day shelf life must be reported as defined: %+v", got)
	}

	cleared, err := h.svc.SetShelfLife(ctx, p.ID, dto.SetShelfLifeRequest{Days: nil})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ShelfLifeDefined {
		t.Error("clearing shelf life should report it undefined")
	}
}

// ---------- Lifecycle ----------

func TestDiscontinueIsTerminal(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	if _, err := h.svc.Discontinue(ctx, p.ID); err != nil {
		t.Fatalf("Discontinue() = %v", err)
	}
	// Terminal: no reactivation.
	if _, err := h.svc.Activate(ctx, p.ID); err == nil {
		t.Fatal("a discontinued product was reactivated")
	}
	if !h.events.has(entity.EventProductDiscontinued) {
		t.Error("no ProductDiscontinued event")
	}
}

func TestActivateThenDeactivate(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())
	p := h.create(t, ctx, "SKU-1", "Widget")

	activated, err := h.svc.Activate(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if activated.Status != entity.StatusActive.String() {
		t.Errorf("status = %q, want ACTIVE", activated.Status)
	}

	deactivated, err := h.svc.Deactivate(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deactivated.Status != entity.StatusDraft.String() {
		t.Errorf("status = %q, want DRAFT", deactivated.Status)
	}
}

// ---------- Update on a missing product ----------

func TestMutateOnMissingProductIsNotFound(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New())

	_, err := h.svc.Activate(ctx, uuid.New())
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Fatalf("code = %s, want NOT_FOUND", code)
	}
}
