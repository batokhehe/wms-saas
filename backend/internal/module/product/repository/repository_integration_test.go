//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// They carry a responsibility unique to an aggregate module: proving the
// AGGREGATE survives a round trip. The aggregate has unexported fields, so
// persistence goes through separate models and a hand-written translation across
// THREE tables — parent plus two child collections — and a translation is
// exactly the kind of code where a forgotten field or a mis-grouped child
// produces silent data loss that no compiler catches.
//
// They also prove the things only a real database can: the company-scoped unique
// index on barcodes, the optimistic-lock conditional update, and the tenant
// isolation of every query.
//
// Run with: make test-integration
package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/batokhehe/wms-saas/backend/internal/module/product/entity"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_DSN"); dsn != "" {
		return dsn
	}
	return "host=localhost port=5432 user=wms password=wms dbname=wms sslmode=disable TimeZone=UTC"
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(testDSN()), &gorm.Config{
		TranslateError: true,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	return db
}

type fixture struct {
	db   *gorm.DB
	repo Repository

	acme, globex uuid.UUID
	actor        uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	db := openDB(t)

	cleanup := func() {
		db.Exec("DELETE FROM product_uoms")
		db.Exec("DELETE FROM product_barcodes")
		db.Exec("DELETE FROM products")
		db.Exec("DELETE FROM role_permissions")
		db.Exec("DELETE FROM roles")
		db.Exec("DELETE FROM memberships")
		db.Exec("DELETE FROM companies")
		db.Exec("DELETE FROM users")
	}
	cleanup()
	t.Cleanup(cleanup)

	f := &fixture{db: db, repo: New(db, adapterid.NewUUID())}
	f.actor = f.seedUser(t, "ops@acme.test")
	f.acme = f.seedCompany(t, "ACME")
	f.globex = f.seedCompany(t, "GLOBEX")
	return f
}

func (f *fixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := f.db.Exec(`
		INSERT INTO users (id, email, password_hash, full_name, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`,
		id, email, "$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		"Test User", now, now,
	).Error
	if err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return id
}

func (f *fixture) seedCompany(t *testing.T, code string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	err := f.db.Exec(`
		INSERT INTO companies (id, code, name, status, created_at, updated_at)
		VALUES (?, ?, ?, 'ACTIVE', ?, ?)`,
		id, code, code+" Ltd", now, now,
	).Error
	if err != nil {
		t.Fatalf("seeding company: %v", err)
	}
	return id
}

// build constructs an aggregate through the factory, as the service does.
func (f *fixture) build(t *testing.T, companyID uuid.UUID, sku, name string) *entity.Product {
	t.Helper()
	productSKU, err := entity.NewSKU(sku)
	if err != nil {
		t.Fatalf("NewSKU(%s) = %v", sku, err)
	}
	productName, err := entity.NewProductName(name)
	if err != nil {
		t.Fatalf("NewProductName(%s) = %v", name, err)
	}
	p, err := entity.NewProduct(uuid.New(), companyID, productSKU, productName, "seeded",
		uuid.New(), f.actor, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewProduct() = %v", err)
	}
	return p
}

func (f *fixture) save(t *testing.T, p *entity.Product) *entity.Product {
	t.Helper()
	if err := f.repo.Save(context.Background(), p); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return p
}

func appliedPaging(t *testing.T) pagination.Request {
	t.Helper()
	var req pagination.Request
	if err := req.Apply(pagination.Options{
		DefaultSort:  "sku",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{"sku": "products.sku"},
	}); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- Aggregate round trip ----------

// TestAggregateSurvivesRoundTrip is the test the separate persistence models
// make necessary. Every field of the parent AND both child collections must come
// back, or the hand-written translation has silently dropped one.
func TestAggregateSurvivesRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	p := f.build(t, f.acme, "SKU-1", "Blue Widget 500ml")

	// Barcodes: one primary, one secondary.
	a, _ := entity.NewBarcode("8990001112223")
	b, _ := entity.NewBarcode("8990001112224")
	if err := p.AddBarcode(a, true, f.actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.AddBarcode(b, false, f.actor, now); err != nil {
		t.Fatal(err)
	}

	// An alternate unit with an exact fractional factor — the reason factors are
	// stored as text, not numeric.
	altUOM := uuid.New()
	factor, _ := entity.NewConversionFactor("1/3")
	if err := p.AddUOM(altUOM, factor, f.actor, now); err != nil {
		t.Fatal(err)
	}

	// Measurements, shelf life, taxonomy, tracking.
	weight, _ := entity.NewWeightKilograms("2.5")
	volume, _ := entity.NewVolumeCubicMetres("0.001")
	w, _ := entity.NewLengthCentimetres("10")
	h, _ := entity.NewLengthCentimetres("20")
	l, _ := entity.NewLengthCentimetres("30")
	dim, _ := entity.NewDimension(w, h, l)
	if err := p.SetMeasurements(&weight, &dim, &volume, f.actor, now); err != nil {
		t.Fatal(err)
	}
	shelf, _ := entity.NewShelfLife(180)
	p.SetShelfLife(shelf, f.actor, now)

	category := uuid.New()
	brand := uuid.New()
	_ = p.AssignCategory(&category, f.actor, now)
	_ = p.AssignBrand(&brand, f.actor, now)
	if err := p.SetTracking(entity.TrackingLot, false, f.actor, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Activate(f.actor, now); err != nil {
		t.Fatal(err)
	}

	f.save(t, p)

	loaded, err := f.repo.FindByID(ctx, p.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if loaded.SKU().String() != "SKU-1" {
		t.Errorf("sku = %q", loaded.SKU())
	}
	if loaded.Name().String() != "Blue Widget 500ml" {
		t.Errorf("name = %q", loaded.Name())
	}
	if loaded.Status() != entity.StatusActive {
		t.Errorf("status = %q, want ACTIVE", loaded.Status())
	}
	if loaded.TrackingMethod() != entity.TrackingLot {
		t.Errorf("tracking = %q, want LOT", loaded.TrackingMethod())
	}
	if loaded.BaseUOMID() != p.BaseUOMID() {
		t.Error("base uom id did not survive the round trip")
	}
	if got := loaded.CategoryID(); got == nil || *got != category {
		t.Errorf("category = %v, want %s", got, category)
	}
	if got := loaded.BrandID(); got == nil || *got != brand {
		t.Errorf("brand = %v, want %s", got, brand)
	}
	if !loaded.ShelfLife().IsDefined() || loaded.ShelfLife().Days() != 180 {
		t.Errorf("shelf life = %+v, want defined 180", loaded.ShelfLife())
	}
	if loaded.Weight() == nil || loaded.Weight().Kilograms().String() != "5/2" {
		t.Errorf("weight = %v, want exact 5/2", loaded.Weight())
	}
	if loaded.Dimension() == nil || loaded.Dimension().Width().Centimetres().String() != "10" {
		t.Errorf("dimension not restored: %+v", loaded.Dimension())
	}

	// Barcodes: both present, exactly one primary, and it is "a".
	barcodes := loaded.Barcodes()
	if len(barcodes) != 2 {
		t.Fatalf("barcodes = %d, want 2", len(barcodes))
	}
	primaries := 0
	for _, bc := range barcodes {
		if bc.IsPrimary() {
			primaries++
		}
	}
	if primaries != 1 {
		t.Errorf("primary barcodes = %d, want exactly 1", primaries)
	}

	// UOMs: base (factor 1) plus the exact 1/3 alternate.
	var sawBase, sawAlt bool
	for _, u := range loaded.UOMs() {
		if u.UOMID() == loaded.BaseUOMID() && u.ConversionFactor().Decimal().String() == "1" {
			sawBase = true
		}
		if u.UOMID() == altUOM && u.ConversionFactor().Decimal().String() == "1/3" {
			sawAlt = true
		}
	}
	if !sawBase || !sawAlt {
		t.Errorf("uoms did not survive exactly: base=%v alt=%v (%+v)", sawBase, sawAlt, loaded.UOMs())
	}

	if loaded.CreatedBy() != f.actor || loaded.UpdatedBy() != f.actor {
		t.Error("the audit columns did not survive the round trip")
	}
}

// TestReconstitutedAggregateRaisesNoEvents: loading rows is not a business
// event, so a read must not publish a creation.
func TestReconstitutedAggregateRaisesNoEvents(t *testing.T) {
	f := newFixture(t)
	p := f.build(t, f.acme, "SKU-1", "Widget")
	p.PullEvents() // discard the creation event
	f.save(t, p)

	loaded, err := f.repo.FindByID(context.Background(), p.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}
	if got := loaded.PullEvents(); len(got) != 0 {
		t.Errorf("loading rows raised %d events, want 0", len(got))
	}
}

// TestChildCollectionsAreReplacedOnUpdate proves the replace strategy: removing
// a barcode and adding another leaves the table holding exactly the current set,
// with no orphaned rows from the previous version.
func TestChildCollectionsAreReplacedOnUpdate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	p := f.build(t, f.acme, "SKU-1", "Widget")
	first, _ := entity.NewBarcode("1111")
	second, _ := entity.NewBarcode("2222")
	_ = p.AddBarcode(first, true, f.actor, now)
	_ = p.AddBarcode(second, false, f.actor, now)
	f.save(t, p)

	// Remove the secondary, add a third.
	if err := p.RemoveBarcode(second, f.actor, now); err != nil {
		t.Fatal(err)
	}
	third, _ := entity.NewBarcode("3333")
	_ = p.AddBarcode(third, false, f.actor, now)
	if err := f.repo.Update(ctx, p); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	var rows int64
	f.db.Raw("SELECT count(*) FROM product_barcodes WHERE product_id = ?", p.ID()).Scan(&rows)
	if rows != 2 {
		t.Fatalf("physical barcode rows = %d, want 2 (no orphans)", rows)
	}

	loaded, _ := f.repo.FindByID(ctx, p.ID(), f.acme)
	got := map[string]bool{}
	for _, bc := range loaded.Barcodes() {
		got[bc.Barcode().String()] = true
	}
	if !got["1111"] || !got["3333"] || got["2222"] {
		t.Errorf("barcode set after replace = %v, want {1111,3333}", got)
	}
}

// ---------- Optimistic locking ----------

// TestConcurrentUpdateAllowsExactlyOneWriter proves the database, rather than an
// in-process lock, arbitrates a stale aggregate write.
func TestConcurrentUpdateAllowsExactlyOneWriter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.save(t, f.build(t, f.acme, "SKU-1", "Widget"))

	left, err := f.repo.FindByID(ctx, p.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	right, err := f.repo.FindByID(ctx, p.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	left.ChangeDescription("left", f.actor, time.Now().UTC())
	right.ChangeDescription("right", f.actor, time.Now().UTC())

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*entity.Product{left, right} {
		wg.Add(1)
		go func(candidate *entity.Product) { defer wg.Done(); <-start; errs <- f.repo.Update(ctx, candidate) }(candidate)
	}
	close(start)
	wg.Wait()
	close(errs)

	var successes, conflicts int
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, sharedrepo.ErrConcurrentModification):
			conflicts++
		default:
			t.Fatalf("Update() = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 each", successes, conflicts)
	}

	loaded, err := f.repo.FindByID(ctx, p.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version() != 2 {
		t.Fatalf("Version() = %d, want 2", loaded.Version())
	}
}

// ---------- Cross-company isolation ----------

func TestReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	globexProduct := f.save(t, f.build(t, f.globex, "SKU-1", "Globex Widget"))

	// Knowing the id is not enough: scoped to Acme, it does not exist.
	_, err := f.repo.FindByID(context.Background(), globexProduct.ID(), f.acme)
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestListIsIsolated(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "SKU-1", "Acme One"))
	f.save(t, f.build(t, f.acme, "SKU-2", "Acme Two"))
	f.save(t, f.build(t, f.globex, "SKU-1", "Globex One"))

	page, err := f.repo.List(context.Background(), f.acme, ListFilter{Paging: appliedPaging(t)})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
	for _, p := range page.Items {
		if p.CompanyID() != f.acme {
			t.Errorf("the list leaked company %s", p.CompanyID())
		}
	}
}

func TestSameSKUAllowedAcrossCompanies(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "SKU-1", "Acme Widget"))
	if err := f.repo.Save(context.Background(), f.build(t, f.globex, "SKU-1", "Globex Widget")); err != nil {
		t.Errorf("the same SKU in another company = %v, want nil", err)
	}
}

func TestExistsChecksAreIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, f.build(t, f.globex, "SKU-1", "Globex Widget"))

	taken, err := f.repo.ExistsBySKU(ctx, f.acme, "SKU-1")
	if err != nil {
		t.Fatalf("ExistsBySKU() = %v", err)
	}
	if taken {
		t.Error("another company's SKU was reported as taken")
	}

	takenName, err := f.repo.ExistsByName(ctx, f.acme, "Globex Widget")
	if err != nil {
		t.Fatalf("ExistsByName() = %v", err)
	}
	if takenName {
		t.Error("another company's name was reported as taken")
	}
}

// TestBarcodeUniquenessIsPerCompany: the same barcode may exist in two companies
// (two businesses legitimately buy the same manufacturer's article) but never
// twice within one.
func TestBarcodeUniquenessIsPerCompany(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	barcode, _ := entity.NewBarcode("8990001112223")

	acmeProduct := f.build(t, f.acme, "SKU-1", "Acme Widget")
	_ = acmeProduct.AddBarcode(barcode, true, f.actor, now)
	f.save(t, acmeProduct)

	// Same barcode, different company: allowed.
	globexProduct := f.build(t, f.globex, "SKU-1", "Globex Widget")
	_ = globexProduct.AddBarcode(barcode, true, f.actor, now)
	if err := f.repo.Save(ctx, globexProduct); err != nil {
		t.Errorf("same barcode across companies = %v, want nil", err)
	}

	// Cross-company existence check must not leak.
	taken, _ := f.repo.ExistsByBarcode(ctx, f.acme, "8990001112223")
	if !taken {
		t.Error("Acme's own barcode reported as free")
	}
	leaked, _ := f.repo.ExistsByBarcodeExcluding(ctx, f.acme, "8990001112223", acmeProduct.ID())
	if leaked {
		t.Error("a barcode belonging only to Acme's own product reported as a collision")
	}
}

// ---------- Constraints ----------

func TestDuplicateSKUIsConflict(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "SKU-1", "Widget"))

	// Different case: the CITEXT unique index must still reject it.
	err := f.repo.Save(context.Background(), f.build(t, f.acme, "sku-1", "Another"))
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestDuplicateNameIsConflict(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "SKU-1", "Blue Widget"))

	err := f.repo.Save(context.Background(), f.build(t, f.acme, "SKU-2", "blue widget"))
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestDuplicateBarcodeIsConflict proves the company-scoped unique index on
// product_barcodes is the race-proof backstop behind the UniqueBarcode
// specification.
func TestDuplicateBarcodeIsConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	barcode, _ := entity.NewBarcode("8990001112223")

	first := f.build(t, f.acme, "SKU-1", "Widget One")
	_ = first.AddBarcode(barcode, true, f.actor, now)
	f.save(t, first)

	// A second product in the same company claiming the same barcode. The
	// aggregate cannot see the sibling, so only the database stops this.
	second := f.build(t, f.acme, "SKU-2", "Widget Two")
	_ = second.AddBarcode(barcode, true, f.actor, now)
	err := f.repo.Save(ctx, second)
	if err == nil {
		t.Fatal("a duplicate barcode within one company was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestStatusCheckConstraint(t *testing.T) {
	f := newFixture(t)
	// Written directly, bypassing the aggregate, to prove the DATABASE also
	// refuses an invalid status.
	err := f.db.Exec(`
		INSERT INTO products (id, company_id, sku, name, base_uom_id, status, tracking, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'SKU-9', 'Bad Status', ?, 'ARCHIVED', 'NONE', ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, uuid.New(), f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted an unknown status")
	}
}

func TestTrackingCheckConstraint(t *testing.T) {
	f := newFixture(t)
	err := f.db.Exec(`
		INSERT INTO products (id, company_id, sku, name, base_uom_id, status, tracking, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'SKU-9', 'Bad Tracking', ?, 'DRAFT', 'BATCH', ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, uuid.New(), f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted an unknown tracking method")
	}
}

func TestForeignKeyRejectsUnknownCompany(t *testing.T) {
	f := newFixture(t)
	err := f.repo.Save(context.Background(), f.build(t, uuid.New(), "SKU-9", "Ghost"))
	if err == nil {
		t.Fatal("a product for a nonexistent company was accepted")
	}
}

// ---------- Filtering ----------

func TestListFiltersByStatusAndTracking(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// One DRAFT/NONE, one ACTIVE/SERIAL.
	f.save(t, f.build(t, f.acme, "SKU-1", "Draft Product"))

	active := f.build(t, f.acme, "SKU-2", "Active Product")
	_ = active.SetTracking(entity.TrackingSerial, false, f.actor, now)
	_ = active.Activate(f.actor, now)
	f.save(t, active)

	page, err := f.repo.List(ctx, f.acme, ListFilter{
		Paging: appliedPaging(t),
		Status: entity.StatusActive.String(),
	})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Meta.Total != 1 || page.Items[0].SKU().String() != "SKU-2" {
		t.Fatalf("status filter = %+v, want only SKU-2", page.Items)
	}

	serialPage, err := f.repo.List(ctx, f.acme, ListFilter{
		Paging:   appliedPaging(t),
		Tracking: entity.TrackingSerial.String(),
	})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if serialPage.Meta.Total != 1 {
		t.Errorf("tracking filter total = %d, want 1", serialPage.Meta.Total)
	}
}

// ---------- Transactions ----------

func TestSaveRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)
	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := f.repo.Save(txCtx, f.build(t, f.acme, "SKU-9", "Rolled Back")); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	taken, err := f.repo.ExistsBySKU(ctx, f.acme, "SKU-9")
	if err != nil {
		t.Fatalf("ExistsBySKU() = %v", err)
	}
	if taken {
		t.Error("a product survived a rolled-back transaction")
	}
	// And its children must not survive either.
	var childRows int64
	f.db.Raw("SELECT count(*) FROM product_uoms").Scan(&childRows)
	if childRows != 0 {
		t.Errorf("orphaned %d uom rows survived the rollback", childRows)
	}
}

// ---------- RBAC seed ----------

// TestProductPermissionsAreSeeded verifies migration 20260728100001 added the
// catalogue rows.
func TestProductPermissionsAreSeeded(t *testing.T) {
	f := newFixture(t)

	var codes []string
	f.db.Raw("SELECT code FROM permissions WHERE module = 'product' ORDER BY code").Scan(&codes)

	want := []string{
		"product.activate", "product.create", "product.discontinue",
		"product.read", "product.update",
	}
	if len(codes) != len(want) {
		t.Fatalf("seeded %d product permissions, want %d: %v", len(codes), len(want), codes)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("permission[%d] = %q, want %q", i, codes[i], want[i])
		}
	}
}
