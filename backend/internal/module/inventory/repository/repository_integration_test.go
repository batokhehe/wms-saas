//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// They prove the AGGREGATE survives a round trip: the aggregate has unexported
// fields, so persistence goes through a separate model and a hand-written
// translation, and a translation is exactly the kind of code where a forgotten
// field produces silent data loss no compiler catches. They also prove what only
// a real database can — the three per-tracking-type unique indexes, the
// optimistic-lock conditional update, and the tenant isolation of every query.
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

	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
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

// tenant is the full FK chain a piece of inventory needs: a company, a warehouse
// in it, a location in that warehouse, and a product. inventories foreign-keys
// all four, so each must be a real row.
type tenant struct {
	company   uuid.UUID
	warehouse uuid.UUID
	location  uuid.UUID
	product   uuid.UUID
}

type fixture struct {
	db    *gorm.DB
	repo  Repository
	actor uuid.UUID

	acme, globex tenant
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := openDB(t)

	cleanup := func() {
		db.Exec("DELETE FROM inventories")
		db.Exec("DELETE FROM storage_locations")
		db.Exec("DELETE FROM products")
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
	f.acme = f.seedTenant(t, "ACME")
	f.globex = f.seedTenant(t, "GLOBEX")
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

// seedTenant creates a company and one warehouse, location and product in it,
// so inventory rows have valid foreign keys to reference.
func (f *fixture) seedTenant(t *testing.T, code string) tenant {
	t.Helper()
	now := time.Now().UTC()
	ten := tenant{company: uuid.New(), warehouse: uuid.New(), location: uuid.New(), product: uuid.New()}

	must := func(err error, what string) {
		if err != nil {
			t.Fatalf("seeding %s: %v", what, err)
		}
	}
	must(f.db.Exec(`INSERT INTO companies (id, code, name, status, created_at, updated_at)
		VALUES (?, ?, ?, 'ACTIVE', ?, ?)`, ten.company, code, code+" Ltd", now, now).Error, "company")
	must(f.db.Exec(`INSERT INTO warehouses (id, company_id, code, name, type, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'MAIN', 'ACTIVE', ?, ?, ?, ?)`,
		ten.warehouse, ten.company, code+"-WH", code+" Warehouse", f.actor, f.actor, now, now).Error, "warehouse")
	must(f.db.Exec(`INSERT INTO storage_locations (id, company_id, warehouse_id, code, zone, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'A', 'ACTIVE', ?, ?, ?, ?)`,
		ten.location, ten.company, ten.warehouse, code+"-A-01", f.actor, f.actor, now, now).Error, "location")
	must(f.db.Exec(`INSERT INTO products (id, company_id, sku, name, base_uom_id, status, tracking, version, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'DRAFT', 'NONE', 1, ?, ?, ?, ?)`,
		ten.product, ten.company, code+"-SKU", code+" Product", uuid.New(), f.actor, f.actor, now, now).Error, "product")

	return ten
}

// build constructs an aggregate through the factory, as a service would.
func (f *fixture) build(t *testing.T, ten tenant, tracking entity.TrackingType, lot, serial string, onHand int64) *entity.Inventory {
	t.Helper()
	lotNo := entity.NoLotNumber()
	if lot != "" {
		lotNo, _ = entity.NewLotNumber(lot)
	}
	serialNo := entity.NoSerialNumber()
	if serial != "" {
		serialNo, _ = entity.NewSerialNumber(serial)
	}
	inv, err := entity.NewInventory(uuid.New(), ten.company, ten.warehouse, ten.location, ten.product,
		tracking, lotNo, serialNo, entity.MustInventoryQuantity(onHand), f.actor, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewInventory() = %v", err)
	}
	return inv
}

func (f *fixture) save(t *testing.T, inv *entity.Inventory) *entity.Inventory {
	t.Helper()
	if err := f.repo.Save(context.Background(), inv); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return inv
}

func appliedPaging(t *testing.T) pagination.Request {
	t.Helper()
	var req pagination.Request
	if err := req.Apply(pagination.Options{
		DefaultSort:  "created_at",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{"created_at": "inventories.created_at"},
	}); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- Round trip ----------

func TestRoundTripNone(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	inv := f.build(t, f.acme, entity.TrackingNone, "", "", 100)
	if err := inv.Reserve(entity.MustQuantity(30), f.actor, now); err != nil {
		t.Fatal(err)
	}
	f.save(t, inv)

	loaded, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}
	if loaded.TrackingType() != entity.TrackingNone || loaded.Status() != entity.StatusActive {
		t.Errorf("tracking/status = %q/%q", loaded.TrackingType(), loaded.Status())
	}
	if loaded.OnHand().Value() != 100 || loaded.Reserved().Value() != 30 || loaded.Available().Value() != 70 {
		t.Errorf("counts on=%d res=%d avl=%d, want 100/30/70", loaded.OnHand().Value(), loaded.Reserved().Value(), loaded.Available().Value())
	}
	if loaded.WarehouseID() != f.acme.warehouse || loaded.LocationID() != f.acme.location || loaded.ProductID() != f.acme.product {
		t.Error("a location/product reference did not survive the round trip")
	}
	if loaded.Version() != 1 || loaded.CreatedBy() != f.actor || loaded.UpdatedBy() != f.actor {
		t.Error("version or audit columns did not survive the round trip")
	}
	if loaded.HasLot() || loaded.HasSerial() {
		t.Error("untracked inventory came back carrying a lot or serial")
	}
	if got := loaded.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
}

func TestRoundTripLotAndSerial(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	lot := f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 50))
	loadedLot, err := f.repo.FindByID(ctx, lot.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	if !loadedLot.HasLot() || loadedLot.Lot().String() != "LOT-A" || loadedLot.OnHand().Value() != 50 {
		t.Errorf("lot round trip wrong: hasLot=%v lot=%q on=%d", loadedLot.HasLot(), loadedLot.Lot(), loadedLot.OnHand().Value())
	}

	serial := f.save(t, f.build(t, f.acme, entity.TrackingSerial, "", "SN-1", 1))
	loadedSerial, err := f.repo.FindByID(ctx, serial.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	if !loadedSerial.HasSerial() || loadedSerial.Serial().String() != "SN-1" || loadedSerial.OnHand().Value() != 1 {
		t.Errorf("serial round trip wrong: hasSerial=%v serial=%q on=%d", loadedSerial.HasSerial(), loadedSerial.Serial(), loadedSerial.OnHand().Value())
	}
}

// ---------- Create then update ----------

func TestSaveCreatesThenUpdates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	inv := f.save(t, f.build(t, f.acme, entity.TrackingNone, "", "", 10))

	loaded, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Increase(entity.MustQuantity(5), f.actor, now); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.Save(ctx, loaded); err != nil {
		t.Fatalf("Save() update = %v", err)
	}

	again, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	if again.OnHand().Value() != 15 {
		t.Errorf("on-hand = %d, want 15", again.OnHand().Value())
	}
	if again.Version() != 2 {
		t.Errorf("version = %d, want 2 after one update", again.Version())
	}
}

// ---------- Optimistic locking ----------

func TestConcurrentSaveAllowsExactlyOneWriter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	inv := f.save(t, f.build(t, f.acme, entity.TrackingNone, "", "", 100))

	left, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	right, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if err != nil {
		t.Fatal(err)
	}
	_ = left.Increase(entity.MustQuantity(1), f.actor, time.Now().UTC())
	_ = right.Increase(entity.MustQuantity(1), f.actor, time.Now().UTC())

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*entity.Inventory{left, right} {
		wg.Add(1)
		go func(c *entity.Inventory) { defer wg.Done(); <-start; errs <- f.repo.Save(ctx, c) }(candidate)
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
			t.Fatalf("Save() = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1 each", successes, conflicts)
	}
	loaded, _ := f.repo.FindByID(ctx, inv.ID(), f.acme.company)
	if loaded.Version() != 2 {
		t.Fatalf("version = %d, want 2", loaded.Version())
	}
}

// ---------- Finds ----------

func TestFindByProductLocation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 10))
	f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-B", "", 20))

	records, err := f.repo.FindByProductLocation(ctx, f.acme.company, f.acme.product, f.acme.location)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2 (one per lot)", len(records))
	}
	for _, r := range records {
		if r.CompanyID() != f.acme.company {
			t.Error("FindByProductLocation leaked another company")
		}
	}
}

func TestFindByLotAndSerial(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 10))
	f.save(t, f.build(t, f.acme, entity.TrackingSerial, "", "SN-1", 1))

	if _, err := f.repo.FindByLot(ctx, f.acme.company, f.acme.product, f.acme.location, "LOT-A"); err != nil {
		t.Errorf("FindByLot(existing) = %v", err)
	}
	if _, err := f.repo.FindByLot(ctx, f.acme.company, f.acme.product, f.acme.location, "LOT-Z"); err == nil {
		t.Error("FindByLot(unknown) should be NOT_FOUND")
	}
	if _, err := f.repo.FindBySerial(ctx, f.acme.company, f.acme.product, "SN-1"); err != nil {
		t.Errorf("FindBySerial(existing) = %v", err)
	}
	if _, err := f.repo.FindBySerial(ctx, f.acme.company, f.acme.product, "SN-Z"); err == nil {
		t.Error("FindBySerial(unknown) should be NOT_FOUND")
	}
}

func TestExists(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	before, err := f.repo.Exists(ctx, f.acme.company, f.acme.product, f.acme.location)
	if err != nil || before {
		t.Fatalf("Exists before = %v/%v, want false/nil", before, err)
	}
	f.save(t, f.build(t, f.acme, entity.TrackingNone, "", "", 5))
	after, err := f.repo.Exists(ctx, f.acme.company, f.acme.product, f.acme.location)
	if err != nil || !after {
		t.Fatalf("Exists after = %v/%v, want true/nil", after, err)
	}
}

// ---------- List + filtering ----------

func TestListFiltersAndIsolates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 10))
	locked := f.build(t, f.acme, entity.TrackingLot, "LOT-B", "", 20)
	_ = locked.Lock(f.actor, now)
	f.save(t, locked)
	f.save(t, f.build(t, f.globex, entity.TrackingNone, "", "", 5)) // another tenant

	all, err := f.repo.List(ctx, f.acme.company, ListFilter{Paging: appliedPaging(t)})
	if err != nil {
		t.Fatal(err)
	}
	if all.Meta.Total != 2 {
		t.Fatalf("acme total = %d, want 2 (globex excluded)", all.Meta.Total)
	}

	lockedPage, err := f.repo.List(ctx, f.acme.company, ListFilter{Paging: appliedPaging(t), Status: entity.StatusLocked.String()})
	if err != nil {
		t.Fatal(err)
	}
	if lockedPage.Meta.Total != 1 {
		t.Errorf("locked total = %d, want 1", lockedPage.Meta.Total)
	}
}

// ---------- Tenant isolation ----------

func TestReadIsTenantIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	acmeInv := f.save(t, f.build(t, f.acme, entity.TrackingSerial, "", "SN-XYZ", 1))

	// Knowing the id is not enough: scoped to Globex it does not exist.
	if _, err := f.repo.FindByID(ctx, acmeInv.ID(), f.globex.company); err == nil {
		t.Error("cross-tenant FindByID succeeded")
	} else if apperror.From(err).Code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", apperror.From(err).Code)
	}
	// The serial exists globally, but Globex cannot resolve it.
	if _, err := f.repo.FindBySerial(ctx, f.globex.company, f.globex.product, "SN-XYZ"); err == nil {
		t.Error("cross-tenant FindBySerial resolved another company's serial")
	}
}

// ---------- Uniqueness ----------

func TestNoneUniqueness(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, entity.TrackingNone, "", "", 5))

	err := f.repo.Save(context.Background(), f.build(t, f.acme, entity.TrackingNone, "", "", 9))
	if err == nil {
		t.Fatal("a second NONE record for the same product+location was accepted")
	}
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestLotUniqueness(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 10))

	err := f.repo.Save(context.Background(), f.build(t, f.acme, entity.TrackingLot, "LOT-A", "", 5))
	if err == nil {
		t.Fatal("a duplicate lot in the same product+location was accepted")
	}
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
	// A different lot in the same place is fine.
	if err := f.repo.Save(context.Background(), f.build(t, f.acme, entity.TrackingLot, "LOT-C", "", 5)); err != nil {
		t.Errorf("a distinct lot was rejected: %v", err)
	}
}

func TestSerialUniquenessIsGlobal(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, entity.TrackingSerial, "", "SN-1", 1))

	// Same serial, SAME company, different product — rejected (serial is unique
	// on its own, not per company or product).
	other := f.build(t, f.acme, entity.TrackingSerial, "", "SN-1", 1)
	if err := f.repo.Save(context.Background(), other); err == nil {
		t.Error("a duplicate serial within a company was accepted")
	}

	// Same serial in ANOTHER company — also rejected, because serials are
	// globally unique physical units.
	globexSerial := f.build(t, f.globex, entity.TrackingSerial, "", "SN-1", 1)
	err := f.repo.Save(context.Background(), globexSerial)
	if err == nil {
		t.Fatal("the same serial in another company was accepted")
	}
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

// ---------- Transactions ----------

func TestSaveRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)
	sentinel := apperror.Internal("forced rollback")

	inv := f.build(t, f.acme, entity.TrackingNone, "", "", 7)
	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := f.repo.Save(txCtx, inv); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	if _, err := f.repo.FindByID(ctx, inv.ID(), f.acme.company); err == nil {
		t.Error("an inventory record survived a rolled-back transaction")
	}
}

// ---------- Foreign keys ----------

func TestForeignKeyRejectsUnknownCompany(t *testing.T) {
	f := newFixture(t)
	ghost := tenant{company: uuid.New(), warehouse: f.acme.warehouse, location: f.acme.location, product: f.acme.product}
	err := f.repo.Save(context.Background(), f.build(t, ghost, entity.TrackingNone, "", "", 1))
	if err == nil {
		t.Fatal("an inventory record for a nonexistent company was accepted")
	}
}
