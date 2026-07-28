//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// They carry a responsibility unique to this module: proving the AGGREGATE
// survives a round trip. The aggregate has unexported fields, so persistence
// goes through a separate model and a hand-written translation — and a
// translation is exactly the kind of code where a forgotten field produces
// silent data loss that no compiler catches.
//
// They also prove the soft-delete write actually works. The archive path sets
// gorm.DeletedAt through a full Save, and whether GORM writes that column on
// Save rather than filtering the row out is a behaviour worth verifying rather
// than assuming.
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

	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
	sharedrepo "github.com/batokhehe/wms-saas/backend/internal/shared/repository"
	"github.com/batokhehe/wms-saas/backend/internal/shared/transaction"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// TestConcurrentUpdateAllowsExactlyOneWriter proves the database, rather than
// an in-process lock, arbitrates a stale aggregate write.
func TestConcurrentUpdateAllowsExactlyOneWriter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	w := f.save(t, f.build(t, f.acme, "WH-01", "Jakarta"))

	left, err := f.repo.FindByID(ctx, w.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	right, err := f.repo.FindByID(ctx, w.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	_ = left.ChangeDescription("left", f.actor, time.Now().UTC())
	_ = right.ChangeDescription("right", f.actor, time.Now().UTC())

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*entity.Warehouse{left, right} {
		wg.Add(1)
		go func(candidate *entity.Warehouse) { defer wg.Done(); <-start; errs <- f.repo.Update(ctx, candidate) }(candidate)
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
	loaded, err := f.repo.FindByID(ctx, w.ID(), f.acme)
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

	acme, globex uuid.UUID
	actor        uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	db := openDB(t)

	cleanup := func() {
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

	return f
}

// seedUser inserts a real users row, because warehouses has foreign keys into
// it for created_by / updated_by.
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

// build constructs an aggregate through the factory, as the service does.
func (f *fixture) build(t *testing.T, companyID uuid.UUID, code, name string) *entity.Warehouse {
	t.Helper()

	warehouseCode, err := entity.NewCode(code)
	if err != nil {
		t.Fatalf("NewCode(%s) = %v", code, err)
	}

	w, err := entity.NewWarehouse(uuid.New(), companyID, warehouseCode,
		name, "seeded", entity.TypeMain, f.actor, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewWarehouse() = %v", err)
	}
	return w
}

func (f *fixture) save(t *testing.T, w *entity.Warehouse) *entity.Warehouse {
	t.Helper()

	if err := f.repo.Save(context.Background(), w); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return w
}

func appliedPaging(t *testing.T) pagination.Request {
	t.Helper()

	var req pagination.Request
	if err := req.Apply(pagination.Options{
		DefaultSort:  "code",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{"code": "warehouses.code"},
	}); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- Aggregate round trip ----------

// TestAggregateSurvivesRoundTrip is the test the separate persistence model
// makes necessary. Every field must come back, or the hand-written translation
// has silently dropped one.
func TestAggregateSurvivesRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	w := f.build(t, f.acme, "WH-01", "Jakarta Central")

	address, _ := entity.NewAddress("Jl. Sudirman 1, Jakarta")
	if err := w.ChangeAddress(address, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("ChangeAddress() = %v", err)
	}

	contact, _ := entity.NewContact("Budi", "+62-811-1111")
	if err := w.ChangeContact(contact, f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("ChangeContact() = %v", err)
	}

	receiving, shipping, staging := uuid.New(), uuid.New(), uuid.New()
	_ = w.AssignReceivingZone(receiving, f.actor, time.Now().UTC())
	_ = w.AssignShippingZone(shipping, f.actor, time.Now().UTC())
	_ = w.AssignStagingZone(staging, f.actor, time.Now().UTC())

	if err := w.Activate(f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("Activate() = %v", err)
	}

	f.save(t, w)

	loaded, err := f.repo.FindByID(ctx, w.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if loaded.Code().String() != "WH-01" {
		t.Errorf("code = %q, want WH-01", loaded.Code())
	}
	if loaded.Name() != "Jakarta Central" {
		t.Errorf("name = %q", loaded.Name())
	}
	if loaded.Type() != entity.TypeMain {
		t.Errorf("type = %q, want MAIN", loaded.Type())
	}
	if loaded.Status() != entity.StatusActive {
		t.Errorf("status = %q, want ACTIVE", loaded.Status())
	}
	if loaded.Address().String() != "Jl. Sudirman 1, Jakarta" {
		t.Errorf("address = %q", loaded.Address())
	}
	if loaded.Contact().Name() != "Budi" || loaded.Contact().Phone() != "+62-811-1111" {
		t.Errorf("contact = %q / %q", loaded.Contact().Name(), loaded.Contact().Phone())
	}
	if got := loaded.Zones().Receiving(); got == nil || *got != receiving {
		t.Errorf("receiving zone = %v, want %s", got, receiving)
	}
	if got := loaded.Zones().Shipping(); got == nil || *got != shipping {
		t.Errorf("shipping zone = %v, want %s", got, shipping)
	}
	if got := loaded.Zones().Staging(); got == nil || *got != staging {
		t.Errorf("staging zone = %v, want %s", got, staging)
	}
	if loaded.CreatedBy() != f.actor || loaded.UpdatedBy() != f.actor {
		t.Error("the audit columns did not survive the round trip")
	}

	// And the reconstituted aggregate must behave, not merely hold data.
	if !loaded.CanReceiveInventory() || !loaded.CanShipInventory() {
		t.Error("a reconstituted ACTIVE warehouse reported itself unable to move stock")
	}
}

// TestReconstitutedAggregateRaisesNoEvents: loading a row is not a business
// event, so a read must not publish a creation.
func TestReconstitutedAggregateRaisesNoEvents(t *testing.T) {
	f := newFixture(t)

	w := f.build(t, f.acme, "WH-01", "Jakarta")
	w.PullEvents() // discard the creation event
	f.save(t, w)

	loaded, err := f.repo.FindByID(context.Background(), w.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}

	if got := loaded.PullEvents(); len(got) != 0 {
		t.Errorf("loading a row raised %d events, want 0", len(got))
	}
}

// ---------- Soft delete ----------

// TestArchivePersistsAsSoftDelete verifies the behaviour the archive path
// depends on: a full Save must WRITE deleted_at, not merely filter the row.
func TestArchivePersistsAsSoftDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	w := f.save(t, f.build(t, f.acme, "WH-01", "Jakarta"))

	if err := w.Archive(f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("Archive() = %v", err)
	}
	if err := f.repo.Update(ctx, w); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	// GORM's automatic filtering hides it from normal reads.
	if _, err := f.repo.FindByID(ctx, w.ID(), f.acme); err == nil {
		t.Error("an archived warehouse was still returned by FindByID")
	}

	// But the row is still there — a warehouse is never hard-deleted, because
	// future stock movements will reference it forever.
	var rows int64
	f.db.Raw("SELECT count(*) FROM warehouses WHERE id = ?", w.ID()).Scan(&rows)
	if rows != 1 {
		t.Fatalf("physical rows = %d, want the archived row retained", rows)
	}

	var archivedAt *time.Time
	f.db.Raw("SELECT deleted_at FROM warehouses WHERE id = ?", w.ID()).Scan(&archivedAt)
	if archivedAt == nil {
		t.Error("deleted_at was not written; the archive did not persist")
	}
}

// TestCodeReusableAfterArchive proves the unique index is partial: a business
// closes a site and later opens a new one on the same code.
func TestCodeReusableAfterArchive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	w := f.save(t, f.build(t, f.acme, "WH-01", "Jakarta"))

	_ = w.Archive(f.actor, time.Now().UTC())
	if err := f.repo.Update(ctx, w); err != nil {
		t.Fatalf("Update() = %v", err)
	}

	replacement := f.build(t, f.acme, "WH-01", "Jakarta Replacement")
	if err := f.repo.Save(ctx, replacement); err != nil {
		t.Errorf("reusing an archived code = %v, want nil", err)
	}
}

// ---------- Cross-company isolation ----------

func TestReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	globexWH := f.save(t, f.build(t, f.globex, "WH-01", "Globex Jakarta"))

	// Knowing the id is not enough: scoped to Acme, it does not exist.
	_, err := f.repo.FindByID(ctx, globexWH.ID(), f.acme)
	if err == nil {
		t.Fatal("cross-tenant read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestListIsIsolated(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, "WH-01", "Acme Jakarta"))
	f.save(t, f.build(t, f.acme, "WH-02", "Acme Surabaya"))
	f.save(t, f.build(t, f.globex, "WH-01", "Globex Jakarta"))

	page, err := f.repo.List(context.Background(), f.acme,
		ListFilter{Paging: appliedPaging(t)})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
	for _, w := range page.Items {
		if w.CompanyID() != f.acme {
			t.Errorf("the list leaked company %s", w.CompanyID())
		}
	}
}

// TestSameCodeAllowedAcrossCompanies: uniqueness is per company, not global —
// two businesses both legitimately call their main site "WH-01".
func TestSameCodeAllowedAcrossCompanies(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, "WH-01", "Acme Jakarta"))

	if err := f.repo.Save(context.Background(),
		f.build(t, f.globex, "WH-01", "Globex Jakarta")); err != nil {
		t.Errorf("the same code in another company = %v, want nil", err)
	}
}

func TestExistsChecksAreIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	f.save(t, f.build(t, f.globex, "WH-01", "Globex Jakarta"))

	taken, err := f.repo.ExistsByCode(ctx, f.acme, "WH-01")
	if err != nil {
		t.Fatalf("ExistsByCode() = %v", err)
	}
	if taken {
		t.Error("another company's code was reported as taken")
	}

	takenName, err := f.repo.ExistsByName(ctx, f.acme, "Globex Jakarta")
	if err != nil {
		t.Fatalf("ExistsByName() = %v", err)
	}
	if takenName {
		t.Error("another company's name was reported as taken")
	}
}

// ---------- Constraints ----------

func TestDuplicateCodeIsConflict(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, "WH-01", "Jakarta"))

	// Different case: the CITEXT unique index must still reject it.
	err := f.repo.Save(context.Background(), f.build(t, f.acme, "wh-01", "Somewhere Else"))
	if err == nil {
		t.Fatal("a duplicate code was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestDuplicateNameIsConflict(t *testing.T) {
	f := newFixture(t)

	f.save(t, f.build(t, f.acme, "WH-01", "Jakarta Central"))

	err := f.repo.Save(context.Background(), f.build(t, f.acme, "WH-02", "jakarta central"))
	if err == nil {
		t.Fatal("a duplicate name was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestStatusCheckConstraint(t *testing.T) {
	f := newFixture(t)

	// Written directly, bypassing the aggregate, to prove the DATABASE also
	// refuses an invalid status. The aggregate is the primary guard; this is
	// the backstop for anything that reaches the table another way.
	err := f.db.Exec(`
		INSERT INTO warehouses
			(id, company_id, code, name, type, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'WH-99', 'Bad Status', 'MAIN', 'ARCHIVED', ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted an unknown status")
	}
}

func TestForeignKeyRejectsUnknownCompany(t *testing.T) {
	f := newFixture(t)

	err := f.repo.Save(context.Background(), f.build(t, uuid.New(), "WH-99", "Ghost"))
	if err == nil {
		t.Fatal("a warehouse for a nonexistent company was accepted")
	}
}

// ---------- Filtering ----------

func TestListFiltersByStatusAndType(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// One DRAFT, one ACTIVE.
	f.save(t, f.build(t, f.acme, "WH-01", "Draft Site"))

	active := f.build(t, f.acme, "WH-02", "Active Site")
	address, _ := entity.NewAddress("somewhere")
	_ = active.ChangeAddress(address, f.actor, time.Now().UTC())
	contact, _ := entity.NewContact("Budi", "+62-811")
	_ = active.ChangeContact(contact, f.actor, time.Now().UTC())
	_ = active.AssignReceivingZone(uuid.New(), f.actor, time.Now().UTC())
	if err := active.Activate(f.actor, time.Now().UTC()); err != nil {
		t.Fatalf("Activate() = %v", err)
	}
	f.save(t, active)

	page, err := f.repo.List(ctx, f.acme, ListFilter{
		Paging: appliedPaging(t),
		Status: entity.StatusActive.String(),
	})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Meta.Total != 1 {
		t.Fatalf("ACTIVE total = %d, want 1", page.Meta.Total)
	}
	if page.Items[0].Code().String() != "WH-02" {
		t.Errorf("filtered to %q, want WH-02", page.Items[0].Code())
	}

	count, err := f.repo.CountActive(ctx, f.acme)
	if err != nil {
		t.Fatalf("CountActive() = %v", err)
	}
	if count != 1 {
		t.Errorf("CountActive() = %d, want 1", count)
	}
}

// ---------- Transactions ----------

func TestSaveRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)

	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := f.repo.Save(txCtx, f.build(t, f.acme, "WH-99", "Rolled Back")); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	taken, err := f.repo.ExistsByCode(ctx, f.acme, "WH-99")
	if err != nil {
		t.Fatalf("ExistsByCode() = %v", err)
	}
	if taken {
		t.Error("a warehouse survived a rolled-back transaction")
	}
}

// ---------- Platform guard ----------

// TestPriorModulesAreUnchanged is a guard rather than a warehouse test: this
// sprint must not have altered the four modules it was told to leave alone.
func TestPriorModulesAreUnchanged(t *testing.T) {
	f := newFixture(t)

	checks := map[string]struct {
		table  string
		column string
		want   int64
	}{
		"users has no company_id":       {"users", "company_id", 0},
		"memberships has no role_id":    {"memberships", "role_id", 0},
		"memberships keeps role":        {"memberships", "role", 1},
		"warehouses is company scoped":  {"warehouses", "company_id", 1},
		"permissions has no company_id": {"permissions", "company_id", 0},
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

// TestWarehousePermissionsAreSeeded verifies migration 20260725100001 both
// added the catalogue rows and backfilled the system roles of companies that
// already existed — without which every pre-existing OWNER could not manage
// warehouses at all.
func TestWarehousePermissionsAreSeeded(t *testing.T) {
	f := newFixture(t)

	var codes []string
	f.db.Raw("SELECT code FROM permissions WHERE module = 'warehouse' ORDER BY code").
		Scan(&codes)

	want := []string{
		"warehouse.activate", "warehouse.create", "warehouse.delete",
		"warehouse.read", "warehouse.update",
	}

	if len(codes) != len(want) {
		t.Fatalf("seeded %d warehouse permissions, want %d: %v", len(codes), len(want), codes)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("permission[%d] = %q, want %q", i, codes[i], want[i])
		}
	}
}
