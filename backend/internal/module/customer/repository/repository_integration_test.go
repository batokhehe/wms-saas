//go:build integration

// Repository tests run against a real PostgreSQL instance — the structural
// sibling of the supplier repository suite. They prove the aggregate survives a
// round trip, the company-scoped unique code index, optimistic locking and the
// tenant isolation of every query.
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

	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
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
		db.Exec("DELETE FROM customers")
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

func (f *fixture) build(t *testing.T, companyID uuid.UUID, code, name string) *entity.Customer {
	t.Helper()
	customerCode, err := entity.NewCustomerCode(code)
	if err != nil {
		t.Fatalf("NewCustomerCode(%s) = %v", code, err)
	}
	c, err := entity.NewCustomer(uuid.New(), companyID, customerCode, name,
		entity.NoEmail(), entity.NoPhone(), entity.NoTaxNumber(), entity.EmptyAddress(), f.actor, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewCustomer() = %v", err)
	}
	return c
}

func (f *fixture) save(t *testing.T, c *entity.Customer) *entity.Customer {
	t.Helper()
	if err := f.repo.Save(context.Background(), c); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	return c
}

func appliedPaging(t *testing.T) pagination.Request {
	t.Helper()
	var req pagination.Request
	if err := req.Apply(pagination.Options{
		DefaultSort:  "code",
		DefaultOrder: pagination.OrderAsc,
		AllowedSorts: map[string]string{"code": "customers.code"},
	}); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- Round trip ----------

func TestAggregateSurvivesRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	c := f.build(t, f.acme, "CUS-1", "Acme Retail")
	email, _ := entity.NewEmail("sales@acme.test")
	phone, _ := entity.NewPhone("+62-811-1111")
	tax, _ := entity.NewTaxNumber("NPWP-99")
	addr, _ := entity.NewAddress("Jl. Sudirman 1", "Jakarta", "DKI", "ID", "10110")
	if err := c.Update("Acme Retail International", email, phone, tax, addr, f.actor, now); err != nil {
		t.Fatal(err)
	}
	c.Deactivate(f.actor, now)
	f.save(t, c)

	loaded, err := f.repo.FindByID(ctx, c.ID(), f.acme)
	if err != nil {
		t.Fatalf("FindByID() = %v", err)
	}
	if loaded.Code().String() != "CUS-1" || loaded.Name() != "Acme Retail International" {
		t.Errorf("code/name wrong: %q / %q", loaded.Code(), loaded.Name())
	}
	if loaded.Status() != entity.StatusInactive {
		t.Errorf("status = %q, want INACTIVE", loaded.Status())
	}
	if loaded.Email().String() != "sales@acme.test" || loaded.Phone().String() != "+62-811-1111" {
		t.Errorf("contact wrong: %q / %q", loaded.Email(), loaded.Phone())
	}
	if loaded.TaxNumber().String() != "NPWP-99" {
		t.Errorf("tax number wrong: %q", loaded.TaxNumber())
	}
	a := loaded.Address()
	if a.Street() != "Jl. Sudirman 1" || a.City() != "Jakarta" || a.Country() != "ID" || a.PostalCode() != "10110" {
		t.Errorf("address did not survive the round trip: %+v", a)
	}
	if loaded.CreatedBy() != f.actor || loaded.UpdatedBy() != f.actor {
		t.Error("audit columns did not survive")
	}
	if got := loaded.PullEvents(); len(got) != 0 {
		t.Errorf("reconstitution raised %d events, want 0", len(got))
	}
}

func TestUnsetContactStoredAsNull(t *testing.T) {
	f := newFixture(t)
	c := f.save(t, f.build(t, f.acme, "CUS-1", "No Contact"))

	var email, phone *string
	f.db.Raw("SELECT email FROM customers WHERE id = ?", c.ID()).Scan(&email)
	f.db.Raw("SELECT phone FROM customers WHERE id = ?", c.ID()).Scan(&phone)
	if email != nil || phone != nil {
		t.Errorf("unset contact stored as non-NULL: email=%v phone=%v", email, phone)
	}

	loaded, _ := f.repo.FindByID(context.Background(), c.ID(), f.acme)
	if !loaded.Email().IsZero() || !loaded.Phone().IsZero() {
		t.Error("a NULL contact did not reconstitute as a zero value object")
	}
}

// ---------- Optimistic locking ----------

func TestConcurrentUpdateAllowsExactlyOneWriter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	c := f.save(t, f.build(t, f.acme, "CUS-1", "Acme"))

	left, err := f.repo.FindByID(ctx, c.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	right, err := f.repo.FindByID(ctx, c.ID(), f.acme)
	if err != nil {
		t.Fatal(err)
	}
	_ = left.Update("Left", entity.NoEmail(), entity.NoPhone(), entity.NoTaxNumber(), entity.EmptyAddress(), f.actor, time.Now().UTC())
	_ = right.Update("Right", entity.NoEmail(), entity.NoPhone(), entity.NoTaxNumber(), entity.EmptyAddress(), f.actor, time.Now().UTC())

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []*entity.Customer{left, right} {
		wg.Add(1)
		go func(c *entity.Customer) { defer wg.Done(); <-start; errs <- f.repo.Update(ctx, c) }(candidate)
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
	loaded, _ := f.repo.FindByID(ctx, c.ID(), f.acme)
	if loaded.Version() != 2 {
		t.Fatalf("Version() = %d, want 2", loaded.Version())
	}
}

// ---------- Tenant isolation ----------

func TestReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	globexCustomer := f.save(t, f.build(t, f.globex, "CUS-1", "Globex"))

	_, err := f.repo.FindByID(context.Background(), globexCustomer.ID(), f.acme)
	if apperror.From(err).Code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", apperror.From(err).Code)
	}
}

func TestListIsIsolated(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "CUS-1", "Acme One"))
	f.save(t, f.build(t, f.acme, "CUS-2", "Acme Two"))
	f.save(t, f.build(t, f.globex, "CUS-1", "Globex One"))

	page, err := f.repo.List(context.Background(), f.acme, ListFilter{Paging: appliedPaging(t)})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
	for _, c := range page.Items {
		if c.CompanyID() != f.acme {
			t.Errorf("list leaked company %s", c.CompanyID())
		}
	}
}

func TestSameCodeAllowedAcrossCompanies(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "CUS-1", "Acme"))
	if err := f.repo.Save(context.Background(), f.build(t, f.globex, "CUS-1", "Globex")); err != nil {
		t.Errorf("the same code in another company = %v, want nil", err)
	}
}

func TestExistsByCodeIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.save(t, f.build(t, f.globex, "CUS-1", "Globex"))

	taken, err := f.repo.ExistsByCode(ctx, f.acme, "CUS-1")
	if err != nil {
		t.Fatal(err)
	}
	if taken {
		t.Error("another company's code was reported as taken")
	}
}

// ---------- Constraints ----------

func TestDuplicateCodeIsConflict(t *testing.T) {
	f := newFixture(t)
	f.save(t, f.build(t, f.acme, "CUS-1", "Acme"))

	// Different case: the CITEXT unique index must still reject it.
	err := f.repo.Save(context.Background(), f.build(t, f.acme, "cus-1", "Acme Two"))
	if apperror.From(err).Code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", apperror.From(err).Code)
	}
}

func TestStatusCheckConstraint(t *testing.T) {
	f := newFixture(t)
	err := f.db.Exec(`
		INSERT INTO customers (id, company_id, code, name, status, created_by, updated_by, created_at, updated_at)
		VALUES (?, ?, 'CUS-9', 'Bad Status', 'ARCHIVED', ?, ?, NOW(), NOW())`,
		uuid.New(), f.acme, f.actor, f.actor,
	).Error
	if err == nil {
		t.Error("the database accepted an unknown status")
	}
}

func TestForeignKeyRejectsUnknownCompany(t *testing.T) {
	f := newFixture(t)
	err := f.repo.Save(context.Background(), f.build(t, uuid.New(), "CUS-9", "Ghost"))
	if err == nil {
		t.Fatal("a customer for a nonexistent company was accepted")
	}
}

// ---------- Filtering + FindByCode ----------

func TestListFiltersByStatusAndFindByCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()

	f.save(t, f.build(t, f.acme, "CUS-1", "Active One"))
	inactive := f.build(t, f.acme, "CUS-2", "Inactive One")
	inactive.Deactivate(f.actor, now)
	f.save(t, inactive)

	page, err := f.repo.List(ctx, f.acme, ListFilter{Paging: appliedPaging(t), Status: entity.StatusInactive.String()})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.Total != 1 || page.Items[0].Code().String() != "CUS-2" {
		t.Fatalf("status filter = %+v, want only CUS-2", page.Items)
	}

	found, err := f.repo.FindByCode(ctx, f.acme, "cus-1")
	if err != nil {
		t.Fatalf("FindByCode() = %v", err)
	}
	if found.Name() != "Active One" {
		t.Errorf("FindByCode resolved %q", found.Name())
	}
}

// ---------- Transactions ----------

func TestSaveRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)
	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := f.repo.Save(txCtx, f.build(t, f.acme, "CUS-9", "Rolled Back")); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}
	taken, err := f.repo.ExistsByCode(ctx, f.acme, "CUS-9")
	if err != nil {
		t.Fatal(err)
	}
	if taken {
		t.Error("a customer survived a rolled-back transaction")
	}
}

// ---------- RBAC seed ----------

func TestCustomerPermissionsAreSeeded(t *testing.T) {
	f := newFixture(t)
	var codes []string
	f.db.Raw("SELECT code FROM permissions WHERE module = 'customer' ORDER BY code").Scan(&codes)
	want := []string{"customer.activate", "customer.create", "customer.read", "customer.update"}
	if len(codes) != len(want) {
		t.Fatalf("seeded %d customer permissions, want %d: %v", len(codes), len(want), codes)
	}
	for i := range want {
		if codes[i] != want[i] {
			t.Errorf("permission[%d] = %q, want %q", i, codes[i], want[i])
		}
	}
}
