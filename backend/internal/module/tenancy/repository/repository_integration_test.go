//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// They matter more here than anywhere else in the codebase. The unit tests use
// fakes that reimplement tenant filtering in Go, so they prove the SERVICE asks
// for the right thing — but only these tests prove the generated SQL actually
// isolates tenants. A fake cannot catch a missing WHERE clause.
//
// Run with: make test-integration
package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	authentity "github.com/batokhehe/wms-saas/backend/internal/module/auth/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	adapterid "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/id"
	"github.com/batokhehe/wms-saas/backend/internal/shared/pagination"
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
	db          *gorm.DB
	companies   CompanyRepository
	memberships MembershipRepository

	acme, globex *entity.Company
	alice, bob   uuid.UUID
}

// newFixture builds two tenants with one member each, over a clean slate.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	db := openDB(t)
	ids := adapterid.NewUUID()

	cleanup := func() {
		// Order matters: memberships holds the foreign keys.
		db.Exec("DELETE FROM memberships")
		db.Exec("DELETE FROM companies")
		db.Exec("DELETE FROM users")
	}
	cleanup()
	t.Cleanup(cleanup)

	f := &fixture{
		db:          db,
		companies:   NewCompanyRepository(db, ids),
		memberships: NewMembershipRepository(db, ids),
	}

	ctx := context.Background()

	f.acme = &entity.Company{Code: "ACME", Name: "Acme", Status: entity.CompanyActive}
	f.globex = &entity.Company{Code: "GLOBEX", Name: "Globex", Status: entity.CompanyActive}

	if err := f.companies.Create(ctx, f.acme); err != nil {
		t.Fatalf("seeding Acme: %v", err)
	}
	if err := f.companies.Create(ctx, f.globex); err != nil {
		t.Fatalf("seeding Globex: %v", err)
	}

	f.alice = f.seedUser(t, "alice@acme.test")
	f.bob = f.seedUser(t, "bob@globex.test")

	f.seedMembership(t, f.acme.ID, f.alice, entity.RoleOwner, entity.MembershipActive)
	f.seedMembership(t, f.globex.ID, f.bob, entity.RoleOwner, entity.MembershipActive)

	return f
}

// seedUser inserts a real users row, because memberships has a foreign key into
// it. Written directly rather than through the auth repository so this test
// exercises tenancy only.
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
		t.Fatalf("seeding user %s: %v", email, err)
	}

	return id
}

func (f *fixture) seedMembership(
	t *testing.T,
	companyID, userID uuid.UUID,
	role entity.Role,
	status entity.MembershipStatus,
) *entity.Membership {
	t.Helper()

	m := &entity.Membership{
		CompanyID: companyID, UserID: userID, Role: role, Status: status,
	}
	if status == entity.MembershipActive {
		now := time.Now().UTC()
		m.JoinedAt = &now
	}

	if err := f.memberships.Create(context.Background(), m); err != nil {
		t.Fatalf("seeding membership: %v", err)
	}
	return m
}

func appliedPaging(t *testing.T, opts pagination.Options) pagination.Request {
	t.Helper()

	var req pagination.Request
	if err := req.Apply(opts); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- Cross-company isolation ----------

// TestCompanyReadIsIsolated is the headline guarantee, proven against real SQL.
func TestCompanyReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Alice reads her own company.
	got, err := f.companies.FindAccessible(ctx, f.acme.ID, f.alice)
	if err != nil {
		t.Fatalf("FindAccessible(own) = %v", err)
	}
	if got.Code != "ACME" {
		t.Errorf("code = %q, want ACME", got.Code)
	}

	// Alice cannot read Globex.
	_, err = f.companies.FindAccessible(ctx, f.globex.ID, f.alice)
	if err == nil {
		t.Fatal("cross-tenant company read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestCompanyListIsIsolated(t *testing.T) {
	f := newFixture(t)

	page, err := f.companies.ListAccessible(context.Background(), f.alice,
		dto.ListCompaniesQuery{
			Request: appliedPaging(t, dto.CompanySortOptions()),
		})
	if err != nil {
		t.Fatalf("ListAccessible() = %v", err)
	}

	if page.Meta.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Meta.Total)
	}
	if len(page.Items) != 1 || page.Items[0].ID != f.acme.ID {
		t.Fatalf("ListAccessible() returned %+v, want only Acme", page.Items)
	}
}

func TestCompanyDeleteIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.companies.Delete(ctx, f.globex.ID, f.alice); err == nil {
		t.Fatal("cross-tenant company delete succeeded")
	}

	// Globex must still be there.
	if _, err := f.companies.FindByIDUnscoped(ctx, f.globex.ID); err != nil {
		t.Errorf("Globex was deleted by a non-member: %v", err)
	}
}

func TestMembershipReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	bobMembership, err := f.memberships.FindActiveByUserAndCompany(ctx, f.bob, f.globex.ID)
	if err != nil {
		t.Fatalf("finding Bob's membership: %v", err)
	}

	// Knowing the id is not enough: scoped to Acme, it does not exist.
	_, err = f.memberships.FindByID(ctx, bobMembership.ID, f.acme.ID)
	if err == nil {
		t.Fatal("cross-tenant membership read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestMembershipDeleteIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	bobMembership, _ := f.memberships.FindActiveByUserAndCompany(ctx, f.bob, f.globex.ID)

	if err := f.memberships.Delete(ctx, bobMembership.ID, f.acme.ID); err == nil {
		t.Fatal("cross-tenant membership delete succeeded")
	}

	if _, err := f.memberships.FindActiveByUserAndCompany(ctx, f.bob, f.globex.ID); err != nil {
		t.Errorf("Bob's membership was removed from another tenant: %v", err)
	}
}

func TestMembershipListIsIsolated(t *testing.T) {
	f := newFixture(t)

	page, err := f.memberships.ListByCompany(context.Background(), f.acme.ID,
		dto.ListMembershipsQuery{
			Request: appliedPaging(t, dto.MembershipSortOptions()),
		})
	if err != nil {
		t.Fatalf("ListByCompany() = %v", err)
	}

	if page.Meta.Total != 1 {
		t.Fatalf("total = %d, want 1", page.Meta.Total)
	}
	for _, m := range page.Items {
		if m.CompanyID != f.acme.ID {
			t.Errorf("member list leaked company %s", m.CompanyID)
		}
		if m.UserID == f.bob {
			t.Error("member list leaked another tenant's user")
		}
	}
}

// ---------- Membership status semantics ----------

// TestPendingMembershipGrantsNothing is the SQL-level proof that an invitation
// is not access.
func TestPendingMembershipGrantsNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	carol := f.seedUser(t, "carol@acme.test")
	f.seedMembership(t, f.acme.ID, carol, entity.RoleStaff, entity.MembershipPending)

	// Cannot resolve a context.
	if _, err := f.memberships.FindActiveByUserAndCompany(ctx, carol, f.acme.ID); err == nil {
		t.Error("a PENDING membership resolved as ACTIVE")
	}

	// Cannot read the company — the accessibleTo subquery filters on ACTIVE.
	if _, err := f.companies.FindAccessible(ctx, f.acme.ID, carol); err == nil {
		t.Error("a PENDING member could read the company")
	}

	// Does not appear in their switcher menu.
	mine, err := f.memberships.ListActiveByUser(ctx, carol)
	if err != nil {
		t.Fatalf("ListActiveByUser() = %v", err)
	}
	if len(mine) != 0 {
		t.Errorf("ListActiveByUser() returned %d, want 0 for a PENDING member", len(mine))
	}
}

func TestSuspendedMembershipGrantsNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	carol := f.seedUser(t, "carol@acme.test")
	f.seedMembership(t, f.acme.ID, carol, entity.RoleStaff, entity.MembershipSuspended)

	if _, err := f.companies.FindAccessible(ctx, f.acme.ID, carol); err == nil {
		t.Error("a SUSPENDED member could read the company")
	}
}

// ---------- Multi-company membership ----------

// TestUserInTwoCompanies is the business rule that justifies the whole design:
// a person belongs to many companies, with a different role in each.
func TestUserInTwoCompanies(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	// Alice joins Globex as STAFF while remaining OWNER of Acme.
	f.seedMembership(t, f.globex.ID, f.alice, entity.RoleStaff, entity.MembershipActive)

	mine, err := f.memberships.ListActiveByUser(ctx, f.alice)
	if err != nil {
		t.Fatalf("ListActiveByUser() = %v", err)
	}
	if len(mine) != 2 {
		t.Fatalf("ListActiveByUser() returned %d, want 2", len(mine))
	}

	roles := map[uuid.UUID]entity.Role{}
	for _, m := range mine {
		roles[m.CompanyID] = m.Role
	}
	if roles[f.acme.ID] != entity.RoleOwner {
		t.Errorf("role at Acme = %q, want OWNER", roles[f.acme.ID])
	}
	// Role is a property of the RELATIONSHIP, not the person.
	if roles[f.globex.ID] != entity.RoleStaff {
		t.Errorf("role at Globex = %q, want STAFF", roles[f.globex.ID])
	}

	// Both companies are now readable by her.
	for _, id := range []uuid.UUID{f.acme.ID, f.globex.ID} {
		if _, err := f.companies.FindAccessible(ctx, id, f.alice); err != nil {
			t.Errorf("FindAccessible(%s) = %v", id, err)
		}
	}
}

// ---------- Constraints ----------

func TestDuplicateMembershipIsConflict(t *testing.T) {
	f := newFixture(t)

	err := f.memberships.Create(context.Background(), &entity.Membership{
		CompanyID: f.acme.ID, UserID: f.alice,
		Role: entity.RoleStaff, Status: entity.MembershipPending,
	})
	if err == nil {
		t.Fatal("a duplicate membership was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

// TestMembershipReusableAfterSoftDelete proves the unique index is partial: a
// removed member can be re-invited.
func TestMembershipReusableAfterSoftDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	carol := f.seedUser(t, "carol@acme.test")
	membership := f.seedMembership(t, f.acme.ID, carol, entity.RoleStaff, entity.MembershipActive)

	if err := f.memberships.Delete(ctx, membership.ID, f.acme.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	err := f.memberships.Create(ctx, &entity.Membership{
		CompanyID: f.acme.ID, UserID: carol,
		Role: entity.RoleStaff, Status: entity.MembershipPending,
	})
	if err != nil {
		t.Errorf("re-inviting a removed member = %v, want nil", err)
	}
}

func TestDuplicateCompanyCodeIsConflict(t *testing.T) {
	f := newFixture(t)

	// Different case: the CITEXT unique index must still reject it.
	err := f.companies.Create(context.Background(), &entity.Company{
		Code: "acme", Name: "Impostor", Status: entity.CompanyActive,
	})
	if err == nil {
		t.Fatal("a duplicate company code was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestCompanyCodeReusableAfterSoftDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.companies.Delete(ctx, f.acme.ID, f.alice); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	err := f.companies.Create(ctx, &entity.Company{
		Code: "ACME", Name: "Acme Reborn", Status: entity.CompanyActive,
	})
	if err != nil {
		t.Errorf("re-creating a soft-deleted code = %v, want nil", err)
	}
}

func TestCountOwners(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	owners, err := f.memberships.CountOwners(ctx, f.acme.ID)
	if err != nil {
		t.Fatalf("CountOwners() = %v", err)
	}
	if owners != 1 {
		t.Errorf("owners = %d, want 1", owners)
	}

	// A PENDING owner does not count — they cannot administer anything yet.
	carol := f.seedUser(t, "carol@acme.test")
	f.seedMembership(t, f.acme.ID, carol, entity.RoleOwner, entity.MembershipPending)

	owners, _ = f.memberships.CountOwners(ctx, f.acme.ID)
	if owners != 1 {
		t.Errorf("owners = %d after adding a PENDING owner, want 1", owners)
	}
}

// TestForeignKeyRejectsUnknownUser proves the referential integrity is real,
// not merely assumed by the application.
func TestForeignKeyRejectsUnknownUser(t *testing.T) {
	f := newFixture(t)

	err := f.memberships.Create(context.Background(), &entity.Membership{
		CompanyID: f.acme.ID, UserID: uuid.New(), // no such user
		Role: entity.RoleStaff, Status: entity.MembershipPending,
	})
	if err == nil {
		t.Fatal("a membership for a nonexistent user was accepted")
	}
}

// ---------- Transactions ----------

// TestOnboardingRollsBack proves the company+membership pair is atomic against
// a real transaction. An orphaned company is unreachable by anyone AND
// permanently consumes its code.
func TestOnboardingRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)

	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		company := &entity.Company{
			Code: "NEWCO", Name: "New Co", Status: entity.CompanyActive,
		}
		if err := f.companies.Create(txCtx, company); err != nil {
			return err
		}
		if err := f.memberships.Create(txCtx, &entity.Membership{
			CompanyID: company.ID, UserID: f.alice,
			Role: entity.RoleOwner, Status: entity.MembershipActive,
		}); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	taken, err := f.companies.ExistsByCode(ctx, "NEWCO")
	if err != nil {
		t.Fatalf("ExistsByCode() = %v", err)
	}
	if taken {
		t.Error("an orphaned company survived a rolled-back transaction")
	}
}

// TestSoftDeletedCompanyIsUnreachable: deleting a company hides it even from
// its own members, without destroying the membership history.
func TestSoftDeletedCompanyIsUnreachable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.companies.Delete(ctx, f.acme.ID, f.alice); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	if _, err := f.companies.FindAccessible(ctx, f.acme.ID, f.alice); err == nil {
		t.Error("a soft-deleted company was still readable by its owner")
	}

	// The membership row survives, so who belonged to the company stays
	// auditable after it closes.
	var count int64
	f.db.Raw("SELECT count(*) FROM memberships WHERE company_id = ? AND deleted_at IS NULL",
		f.acme.ID).Scan(&count)
	if count != 1 {
		t.Errorf("membership rows = %d, want the history preserved", count)
	}
}

// TestAuthModuleIsUnaffected is a guard rather than a tenancy test: this sprint
// must not have changed identity. If a users row can still be written and read
// with no company involved, the independence holds.
func TestAuthModuleIsUnaffected(t *testing.T) {
	f := newFixture(t)

	var user authentity.User
	if err := f.db.Where("id = ?", f.alice).First(&user).Error; err != nil {
		t.Fatalf("reading the seeded user: %v", err)
	}
	if user.Email != "alice@acme.test" {
		t.Errorf("email = %q", user.Email)
	}

	// The compile-time proof that users has no company column is that
	// authentity.User has no such field; this asserts the row is readable
	// without any tenant context at all.
	if err := f.db.Exec(
		"SELECT 1 FROM information_schema.columns " +
			"WHERE table_name = 'users' AND column_name = 'company_id'").Error; err != nil {
		t.Fatalf("querying schema: %v", err)
	}

	var columnCount int64
	f.db.Raw("SELECT count(*) FROM information_schema.columns " +
		"WHERE table_name = 'users' AND column_name = 'company_id'").Scan(&columnCount)
	if columnCount != 0 {
		t.Error("users gained a company_id column; identity must stay tenant-independent")
	}
}
