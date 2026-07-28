//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// Three things can only be verified here:
//
//   - the AGGREGATE survives a round trip. Its fields are unexported, so
//     persistence goes through a hand-written translation — exactly the kind of
//     code where a forgotten field causes silent data loss no compiler catches.
//   - the NUMERIC columns preserve decimal precision. The whole reason capacity
//     uses big.Rat rather than float64 is that a round trip through the driver
//     must not round.
//   - the two-level uniqueness actually holds in SQL: code per WAREHOUSE,
//     barcode per COMPANY.
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

	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// TestConcurrentUpdateAllowsExactlyOneWriter is a PostgreSQL race simulation.
func TestConcurrentUpdateAllowsExactlyOneWriter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	l := f.save(t, f.build(t, f.acme, f.acmeWH1, "A-01", ""))
	left, err := f.repo.FindByID(ctx, l.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	right, err := f.repo.FindByID(ctx, l.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	_ = left.ChangePickingPriority(10, f.actor, time.Now().UTC())
	_ = right.ChangePickingPriority(20, f.actor, time.Now().UTC())

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*entity.StorageLocation{left, right} {
		wg.Add(1)
		go func(candidate *entity.StorageLocation) {
			defer wg.Done()
			<-start
			errs <- f.repo.Update(ctx, candidate)
		}(candidate)
	}
	close(start)
	wg.Wait()
	close(errs)
	var successes, conflicts int
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, sharedrepo.ErrConcurrentModification) {
			conflicts++
		} else {
			t.Fatalf("Update() = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 each", successes, conflicts)
	}
	loaded, err := f.repo.FindByID(ctx, l.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version() != 2 {
		t.Fatalf("Version() = %d, want 2", loaded.Version())
	}
}

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

	acme, globex     uuid.UUID
	acmeWH1, acmeWH2 uuid.UUID
	globexWH         uuid.UUID
	actor            uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	db := openDB(t)

	cleanup := func() {
		// Order matters: storage_locations references warehouses (RESTRICT), so
		// it must go first.
		db.Exec("DELETE FROM storage_locations")
		db.Exec("DELETE FROM warehouses")
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

	f.acmeWH1 = f.seedWarehouse(t, f.acme, "WH-01")
	f.acmeWH2 = f.seedWarehouse(t, f.acme, "WH-02")
	f.globexWH = f.seedWarehouse(t, f.globex, "WH-01")

	return f
}

func (f *fixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	now := time.Now().UTC()

	err := f.db.Exec(`
		INSERT INTO users (id, email, password_hash, full_name, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)`,
		id, email,
		"$2a$12$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
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

// seedWarehouse writes a warehouses row directly. Written as SQL rather than
// through the warehouse repository so this suite exercises locations only.
func (f *fixture) seedWarehouse(t *testing.T, companyID uuid.UUID, code string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	now := time.Now().UTC()

	err := f.db.Exec(`
		INSERT INTO warehouses
			(id, company_id, code, name, type, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'MAIN', 'ACTIVE', ?, ?, ?, ?)`,
		id, companyID, code, code+" Site", f.actor, f.actor, now, now,
	).Error
	if err != nil {
		t.Fatalf("seeding warehouse: %v", err)
	}
	return id
}

// build constructs an aggregate through the factory, as the service does.
func (f *fixture) build(
	t *testing.T, companyID, warehouseID uuid.UUID, zone, aisle string,
) *entity.StorageLocation {
	t.Helper()

	coordinate, err := entity.NewCoordinate(zone, aisle, "", "", "")
	if err != nil {
		t.Fatalf("NewCoordinate() = %v", err)
	}

	l, err := entity.NewStorageLocation(
		uuid.New(), companyID, warehouseID, coordinate,
		entity.LocationCode{}, f.actor, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("NewStorageLocation() = %v", err)
	}
	return l
}

func (f *fixture) save(t *testing.T, l *entity.StorageLocation) *entity.StorageLocation {
	t.Helper()

	if err := f.repo.Save(context.Background(), l); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return l
}

func appliedPaging(t *testing.T) pagination.Request {
	t.Helper()

	var req pagination.Request
	if err := req.Apply(pagination.Options{
		DefaultSort:  "code",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{"code": "storage_locations.code"},
	}); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

func mustQuantity(t *testing.T, raw string) entity.Quantity {
	t.Helper()

	q, err := entity.NewQuantity(raw, "test")
	if err != nil {
		t.Fatalf("NewQuantity(%q) = %v", raw, err)
	}
	return q
}

func intPtr(v int) *int { return &v }

// ---------- Aggregate round trip ----------

// TestAggregateSurvivesRoundTrip is the test the separate persistence model
// makes necessary. Every field must come back, or the hand-written translation
// has silently dropped one.
func TestAggregateSurvivesRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	coordinate, err := entity.NewCoordinate("A", "01", "02", "03", "04")
	if err != nil {
		t.Fatalf("NewCoordinate() = %v", err)
	}

	l, err := entity.NewStorageLocation(
		uuid.New(), f.acme, f.acmeWH1, coordinate,
		entity.LocationCode{}, f.actor, time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("NewStorageLocation() = %v", err)
	}

	barcode, _ := entity.NewBarcode("LOC-Abc123")
	if err := l.AssignBarcode(barcode, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("AssignBarcode() = %v", err)
	}

	capacity, _ := entity.NewCapacity(
		mustQuantity(t, "1234.567"), mustQuantity(t, "8.910"), intPtr(6))
	if err := l.ChangeCapacity(capacity, entity.Usage{}, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("ChangeCapacity() = %v", err)
	}

	if err := l.ChangePickingPriority(7, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("ChangePickingPriority() = %v", err)
	}
	if err := l.EnableMixedSKU(f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("EnableMixedSKU() = %v", err)
	}
	if err := l.SetAllowOverflow(true, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("SetAllowOverflow() = %v", err)
	}
	if err := l.Lock("damaged racking", f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("Lock() = %v", err)
	}

	f.save(t, l)

	loaded, err := f.repo.FindByID(ctx, l.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if loaded.Code().String() != "A-01-02-03-04" {
		t.Errorf("code = %q", loaded.Code())
	}
	if got := loaded.Coordinate(); got.Zone() != "A" || got.Bin() != "04" || got.Depth() != 5 {
		t.Errorf("coordinate = %+v, want the full five segments", got)
	}
	// Case is preserved for barcodes, unlike codes and coordinates.
	if loaded.Barcode().String() != "LOC-Abc123" {
		t.Errorf("barcode = %q, want its case preserved", loaded.Barcode())
	}
	if loaded.Status() != entity.StatusLocked {
		t.Errorf("status = %q, want LOCKED", loaded.Status())
	}
	if loaded.PickingPriority() != 7 {
		t.Errorf("priority = %d, want 7", loaded.PickingPriority())
	}
	if !loaded.AllowMixedSKU() || !loaded.AllowOverflow() {
		t.Error("the policy flags did not survive the round trip")
	}
	if loaded.CreatedBy() != f.actor || loaded.UpdatedBy() != f.actor {
		t.Error("the audit columns did not survive the round trip")
	}

	// And the reconstituted aggregate must BEHAVE, not merely hold data.
	if loaded.CanReceiveInventory() || loaded.CanPickInventory() {
		t.Error("a reconstituted LOCKED location reported itself usable")
	}
}

// TestCapacityPrecisionSurvivesTheDatabase is why capacity is big.Rat and
// NUMERIC rather than float64 and double precision.
func TestCapacityPrecisionSurvivesTheDatabase(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	l := f.build(t, f.acme, f.acmeWH1, "A", "01")

	capacity, _ := entity.NewCapacity(
		mustQuantity(t, "1234.567"), mustQuantity(t, "0.001"), intPtr(3))
	if err := l.ChangeCapacity(capacity, entity.Usage{}, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("ChangeCapacity() = %v", err)
	}
	f.save(t, l)

	loaded, err := f.repo.FindByID(ctx, l.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if got := loaded.Capacity().MaxWeight().String(); got != "1234.567" {
		t.Errorf("max_weight = %q, want 1234.567 with no rounding", got)
	}
	if got := loaded.Capacity().MaxVolume().String(); got != "0.001" {
		t.Errorf("max_volume = %q, want 0.001 preserved", got)
	}
	if got := loaded.Capacity().MaxPallet(); got == nil || *got != 3 {
		t.Errorf("max_pallet = %v, want 3", got)
	}
}

// TestUnsetCapacityIsStoredAsNull: NULL means "not measured" and is genuinely
// different from zero.
func TestUnsetCapacityIsStoredAsNull(t *testing.T) {
	f := newFixture(t)

	l := f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	var weight *string
	f.db.Raw("SELECT max_weight FROM storage_locations WHERE id = ?", l.ID()).Scan(&weight)
	if weight != nil {
		t.Errorf("max_weight = %v, want SQL NULL for an unmeasured location", *weight)
	}

	loaded, err := f.repo.FindByID(context.Background(), l.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}
	if !loaded.Capacity().IsUnlimited() {
		t.Error("an unmeasured capacity came back limited")
	}
}

// TestAbsentBarcodeIsStoredAsNull matters because the unique index is partial
// on `barcode IS NOT NULL` — empty strings would collide where NULLs do not.
func TestAbsentBarcodeIsStoredAsNull(t *testing.T) {
	f := newFixture(t)

	first := f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))
	second := f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "02"))

	var nulls int64
	f.db.Raw("SELECT count(*) FROM storage_locations WHERE barcode IS NULL").Scan(&nulls)
	if nulls != 2 {
		t.Errorf("NULL barcodes = %d, want 2 — empty strings would collide", nulls)
	}

	_ = first
	_ = second
}

func TestReconstitutedAggregateRaisesNoEvents(t *testing.T) {
	f := newFixture(t)

	l := f.build(t, f.acme, f.acmeWH1, "A", "01")
	l.PullEvents()
	f.save(t, l)

	loaded, err := f.repo.FindByID(context.Background(), l.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if got := loaded.PullEvents(); len(got) != 0 {
		t.Errorf("loading a row raised %d events, want 0", len(got))
	}
}

// ---------- Two-level uniqueness ----------

func TestCodeUniqueWithinWarehouse(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	// Same warehouse, different case: the CITEXT unique index must reject it.
	err := f.repo.Save(context.Background(), f.build(t, f.acme, f.acmeWH1, "a", "01"))
	if err == nil {
		t.Fatal("a duplicate code in the same warehouse was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestSameCodeAllowedInAnotherWarehouse: aisle numbering restarts at every
// building, so "A-01" in two sites of the SAME company is normal.
func TestSameCodeAllowedInAnotherWarehouse(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	if err := f.repo.Save(context.Background(),
		f.build(t, f.acme, f.acmeWH2, "A", "01")); err != nil {
		t.Errorf("the same code in another warehouse = %v, want nil", err)
	}
}

// TestBarcodeUniqueAcrossWarehouses is the counterpart: barcode uniqueness is
// per COMPANY, because a scanner reads a label with no idea which site it is in.
func TestBarcodeUniqueAcrossWarehouses(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	first := f.build(t, f.acme, f.acmeWH1, "A", "01")
	barcode, _ := entity.NewBarcode("LOC-000123")
	_ = first.AssignBarcode(barcode, f.actor, time.Now().UTC())
	f.save(t, first)

	// A DIFFERENT warehouse in the same company.
	second := f.build(t, f.acme, f.acmeWH2, "B", "01")
	_ = second.AssignBarcode(barcode, f.actor, time.Now().UTC())

	err := f.repo.Save(ctx, second)
	if err == nil {
		t.Fatal("a duplicate barcode across warehouses was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestSameBarcodeAllowedInAnotherCompany(t *testing.T) {
	f := newFixture(t)

	barcode, _ := entity.NewBarcode("LOC-000123")

	first := f.build(t, f.acme, f.acmeWH1, "A", "01")
	_ = first.AssignBarcode(barcode, f.actor, time.Now().UTC())
	f.save(t, first)

	second := f.build(t, f.globex, f.globexWH, "A", "01")
	_ = second.AssignBarcode(barcode, f.actor, time.Now().UTC())

	if err := f.repo.Save(context.Background(), second); err != nil {
		t.Errorf("the same barcode in another company = %v, want nil", err)
	}
}

func TestCodeReusableAfterArchive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	l := f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	_ = l.Archive(f.actor, time.Now().UTC())
	if err := f.repo.Update(ctx, l); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if err := f.repo.Save(ctx, f.build(t, f.acme, f.acmeWH1, "A", "01")); err != nil {
		t.Errorf("reusing an archived code = %v, want nil", err)
	}
}

// ---------- Soft delete ----------

// TestArchivePersistsAsSoftDelete verifies the behaviour the archive path
// depends on: a full Save must WRITE deleted_at, not merely filter the row.
func TestArchivePersistsAsSoftDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	l := f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	if err := l.Archive(f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("Archive() = %v", err)
	}
	if err := f.repo.Update(ctx, l); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	if _, err := f.repo.FindByID(ctx, l.ID(), f.acme); err == nil {
		t.Error("an archived location was still returned by FindByID")
	}

	// The row survives — future stock movements will reference it forever.
	var rows int64
	f.db.Raw("SELECT count(*) FROM storage_locations WHERE id = ?", l.ID()).Scan(&rows)
	if rows != 1 {
		t.Fatalf("physical rows = %d, want the archived row retained", rows)
	}

	var archivedAt *time.Time
	f.db.Raw("SELECT deleted_at FROM storage_locations WHERE id = ?", l.ID()).Scan(&archivedAt)
	if archivedAt == nil {
		t.Error("deleted_at was not written; the archive did not persist")
	}
}

// ---------- Cross-company and cross-warehouse isolation ----------

func TestReadIsIsolatedByCompany(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	globexLoc := f.save(t, f.build(t, f.globex, f.globexWH, "A", "01"))

	_, err := f.repo.FindByID(ctx, globexLoc.ID(), f.acme)
	if err == nil {
		t.Fatal("cross-tenant read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestListIsIsolatedByCompany(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))
	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "02"))
	f.save(t, f.build(t, f.globex, f.globexWH, "A", "01"))

	page, err := f.repo.List(context.Background(), f.acme,
		ListFilter{Paging: appliedPaging(t)})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
	for _, l := range page.Items {
		if l.CompanyID() != f.acme {
			t.Errorf("the list leaked company %s", l.CompanyID())
		}
	}
}

// TestListIsScopedByWarehouse is the second boundary: a company owns many
// warehouses, and an operational read wants one of them.
func TestListIsScopedByWarehouse(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))
	f.save(t, f.build(t, f.acme, f.acmeWH2, "B", "01"))

	page, err := f.repo.List(context.Background(), f.acme, ListFilter{
		Paging:      appliedPaging(t),
		WarehouseID: f.acmeWH1,
	})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if page.Meta.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Meta.Total)
	}
	if page.Items[0].WarehouseID() != f.acmeWH1 {
		t.Error("the warehouse filter leaked another site's location")
	}
}

func TestBarcodeLookupIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	globexLoc := f.build(t, f.globex, f.globexWH, "A", "01")
	barcode, _ := entity.NewBarcode("LOC-000123")
	_ = globexLoc.AssignBarcode(barcode, f.actor, time.Now().UTC())
	f.save(t, globexLoc)

	if _, err := f.repo.FindByBarcode(ctx, f.acme, "LOC-000123"); err == nil {
		t.Error("a barcode resolved across tenants")
	}

	if _, err := f.repo.FindByBarcode(ctx, f.globex, "LOC-000123"); err != nil {
		t.Errorf("the owning company could not resolve its own barcode: %v", err)
	}
}

func TestExistsChecksAreIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.save(t, f.build(t, f.globex, f.globexWH, "A", "01"))

	taken, err := f.repo.ExistsByCode(ctx, f.acme, f.acmeWH1, "A-01")
	if err != nil {
		t.Fatalf("ExistsByCode() = %v", err)
	}
	if taken {
		t.Error("another company's code was reported as taken")
	}
}

// ---------- Constraints ----------

func TestStatusCheckConstraint(t *testing.T) {
	f := newFixture(t)

	// Written directly, bypassing the aggregate, to prove the DATABASE also
	// refuses an invalid status.
	err := f.db.Exec(`
		INSERT INTO storage_locations
			(id, company_id, warehouse_id, code, zone, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, 'X-99', 'X', 'ARCHIVED', ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, f.acmeWH1, f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted an unknown status")
	}
}

func TestNegativeCapacityCheckConstraint(t *testing.T) {
	f := newFixture(t)

	err := f.db.Exec(`
		INSERT INTO storage_locations
			(id, company_id, warehouse_id, code, zone, status, max_weight,
			 created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, 'X-98', 'X', 'ACTIVE', -1, ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, f.acmeWH1, f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted a negative capacity")
	}
}

func TestForeignKeyRejectsUnknownWarehouse(t *testing.T) {
	f := newFixture(t)

	err := f.repo.Save(context.Background(),
		f.build(t, f.acme, uuid.New(), "X", "99"))
	if err == nil {
		t.Fatal("a location in a nonexistent warehouse was accepted")
	}
}

// TestWarehouseCannotBePurgedWhileLocationsExist covers the ON DELETE RESTRICT
// choice: silently destroying tens of thousands of location rows is not
// something an administrative purge should do implicitly.
func TestWarehouseCannotBePurgedWhileLocationsExist(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, f.acmeWH1, "A", "01"))

	err := f.db.Exec("DELETE FROM warehouses WHERE id = ?", f.acmeWH1).Error
	if err == nil {
		t.Error("a warehouse holding locations was hard-deleted")
	}
}

// ---------- Bulk insert ----------

// TestSaveManyInsertsBatch covers the rack-import path: a single aisle is
// hundreds of bins, and inserting them one round trip at a time is the
// difference between a second and a minute.
func TestSaveManyInsertsBatch(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	locations := make([]*entity.StorageLocation, 0, 50)
	for i := 0; i < 50; i++ {
		coordinate, err := entity.NewCoordinate("A", "01", "02", pad(i), "")
		if err != nil {
			t.Fatalf("NewCoordinate() = %v", err)
		}
		l, err := entity.NewStorageLocation(
			uuid.New(), f.acme, f.acmeWH1, coordinate,
			entity.LocationCode{}, f.actor, time.Now().UTC(),
		)
		if err != nil {
			t.Fatalf("NewStorageLocation() = %v", err)
		}
		locations = append(locations, l)
	}

	if err := f.repo.SaveMany(ctx, locations); err != nil {
		t.Fatalf("SaveMany() = %v", err)
	}

	count, err := f.repo.CountByWarehouse(ctx, f.acme, f.acmeWH1)
	if err != nil {
		t.Fatalf("CountByWarehouse() = %v", err)
	}
	if count != 50 {
		t.Errorf("count = %d, want 50", count)
	}
}

func pad(i int) string {
	digits := "0123456789"
	return string([]byte{digits[i/10], digits[i%10]})
}

// ---------- Transactions ----------

func TestSaveRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)

	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := f.repo.Save(txCtx, f.build(t, f.acme, f.acmeWH1, "Z", "99")); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	taken, err := f.repo.ExistsByCode(ctx, f.acme, f.acmeWH1, "Z-99")
	if err != nil {
		t.Fatalf("ExistsByCode() = %v", err)
	}
	if taken {
		t.Error("a location survived a rolled-back transaction")
	}
}

// ---------- Platform guards ----------

// TestPriorModulesAreUnchanged asserts this sprint altered none of the modules
// it was told to leave alone.
func TestPriorModulesAreUnchanged(t *testing.T) {
	f := newFixture(t)

	checks := map[string]struct {
		table  string
		column string
		want   int64
	}{
		"users has no company_id":          {"users", "company_id", 0},
		"memberships has no role_id":       {"memberships", "role_id", 0},
		"permissions has no company_id":    {"permissions", "company_id", 0},
		"warehouses keeps its zone ids":    {"warehouses", "default_receiving_zone_id", 1},
		"warehouses gained no location fk": {"warehouses", "location_id", 0},
		"locations are company scoped":     {"storage_locations", "company_id", 1},
		"locations are warehouse scoped":   {"storage_locations", "warehouse_id", 1},
	}

	for label, check := range checks {
		var count int64
		f.db.Raw(
			"SELECT count(*) FROM information_schema.columns "+
				"WHERE table_name = ? AND column_name = ?",
			check.table, check.column,
		).Scan(&count)

		if count != check.want {
			t.Errorf("%s: found %d columns, want %d", label, count, check.want)
		}
	}
}

func TestLocationPermissionsAreSeeded(t *testing.T) {
	f := newFixture(t)

	var codes []string
	f.db.Raw("SELECT code FROM permissions WHERE module = 'location' ORDER BY code").
		Scan(&codes)

	want := []string{
		"location.create", "location.lock", "location.read", "location.update",
	}

	if len(codes) != len(want) {
		t.Fatalf("seeded %d location permissions, want %d: %v", len(codes), len(want), codes)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("permission[%d] = %q, want %q", i, codes[i], want[i])
		}
	}
}
