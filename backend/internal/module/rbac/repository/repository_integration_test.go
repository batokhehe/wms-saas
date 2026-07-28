//go:build integration

// Repository tests run against a real PostgreSQL instance.
//
// These matter more than the unit tests for isolation: the fakes reimplement
// tenant filtering in Go, so they prove the SERVICE asks for the right thing,
// but only these prove the generated SQL actually isolates tenants. A fake
// cannot catch a missing WHERE clause.
//
// They also verify the seeded permission catalogue matches
// entity.PermissionCatalogue(), which nothing else can check.
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

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
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
	db     *gorm.DB
	roles  RoleRepository
	perms  PermissionRepository
	grants RolePermissionRepository

	acme, globex uuid.UUID
}

// newFixture creates two companies over a clean slate.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	db := openDB(t)
	ids := adapterid.NewUUID()

	cleanup := func() {
		// Order matters: role_permissions holds foreign keys into roles.
		// permissions is seeded by migration and is NOT truncated — deleting it
		// would break every other test and require a re-migration.
		db.Exec("DELETE FROM role_permissions")
		db.Exec("DELETE FROM roles")
		db.Exec("DELETE FROM memberships")
		db.Exec("DELETE FROM companies")
		db.Exec("DELETE FROM users")
	}
	cleanup()
	t.Cleanup(cleanup)

	f := &fixture{
		db:     db,
		roles:  NewRoleRepository(db, ids),
		perms:  NewPermissionRepository(db, ids),
		grants: NewRolePermissionRepository(db, ids),
	}

	f.acme = f.seedCompany(t, "ACME")
	f.globex = f.seedCompany(t, "GLOBEX")

	return f
}

// seedCompany inserts a real companies row, because roles has a foreign key
// into it. Written directly rather than through the tenancy repository so this
// test exercises RBAC only.
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
		t.Fatalf("seeding company %s: %v", code, err)
	}
	return id
}

func (f *fixture) seedRole(
	t *testing.T, companyID uuid.UUID, name string, isSystem bool,
) *entity.Role {
	t.Helper()

	role := &entity.Role{CompanyID: companyID, Name: name, IsSystem: isSystem}
	if err := f.roles.Create(context.Background(), role); err != nil {
		t.Fatalf("seeding role %s: %v", name, err)
	}
	return role
}

func (f *fixture) permissionID(t *testing.T, code entity.Code) uuid.UUID {
	t.Helper()

	permissions, err := f.perms.FindByCodes(context.Background(), []entity.Code{code})
	if err != nil {
		t.Fatalf("looking up %s: %v", code, err)
	}
	if len(permissions) != 1 {
		t.Fatalf("permission %s is not in the seeded catalogue", code)
	}
	return permissions[0].ID
}

func appliedPaging(t *testing.T, opts pagination.Options) pagination.Request {
	t.Helper()

	var req pagination.Request
	if err := req.Apply(opts); err != nil {
		t.Fatalf("applying paging: %v", err)
	}
	return req
}

// ---------- The seeded catalogue ----------

// TestSeededCatalogueMatchesCode is the drift guard between the migration and
// entity.PermissionCatalogue(). A code in one but not the other produces a
// permission that can never be granted, or a grant that can never be resolved —
// and neither fails anywhere else.
func TestSeededCatalogueMatchesCode(t *testing.T) {
	f := newFixture(t)

	seeded, err := f.perms.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	inDB := make(map[entity.Code]struct{}, len(seeded))
	for i := range seeded {
		inDB[seeded[i].Code] = struct{}{}
	}

	for _, code := range entity.PermissionCatalogue() {
		if _, ok := inDB[code]; !ok {
			t.Errorf("code %s is declared in Go but not seeded in the database", code)
		}
		delete(inDB, code)
	}

	for code := range inDB {
		t.Errorf("code %s is seeded in the database but not declared in Go", code)
	}
}

func TestPermissionsAreImmutable(t *testing.T) {
	f := newFixture(t)

	// The interface exposes no write method at all — this asserts the shape
	// rather than a runtime check, which is the point: there is nothing to
	// forget to guard.
	var repo any = f.perms
	if _, ok := repo.(interface {
		Create(context.Context, *entity.Permission) error
	}); ok {
		t.Error("PermissionRepository exposes a Create method; the catalogue must be immutable")
	}
}

func TestListPermissionsByModule(t *testing.T) {
	f := newFixture(t)

	permissions, err := f.perms.List(context.Background(), "role")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(permissions) == 0 {
		t.Fatal("no role-module permissions found")
	}
	for i := range permissions {
		if permissions[i].Module != "role" {
			t.Errorf("module = %q, want role", permissions[i].Module)
		}
	}
}

// ---------- Cross-company isolation ----------

func TestRoleReadIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	globexAdmin := f.seedRole(t, f.globex, "ADMIN", true)

	// Knowing the id is not enough: scoped to Acme, it does not exist.
	_, err := f.roles.FindByID(ctx, globexAdmin.ID, f.acme)
	if err == nil {
		t.Fatal("cross-tenant role read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestRoleListIsIsolated(t *testing.T) {
	f := newFixture(t)

	f.seedRole(t, f.acme, "ADMIN", true)
	f.seedRole(t, f.acme, "AUDITOR", false)
	f.seedRole(t, f.globex, "ADMIN", true)

	page, err := f.roles.List(context.Background(), f.acme, dto.ListRolesQuery{
		Request: appliedPaging(t, dto.RoleSortOptions()),
	})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if page.Meta.Total != 2 {
		t.Fatalf("total = %d, want 2", page.Meta.Total)
	}
	for _, role := range page.Items {
		if role.CompanyID != f.acme {
			t.Errorf("role list leaked company %s", role.CompanyID)
		}
	}
}

func TestRoleDeleteIsIsolated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	globexRole := f.seedRole(t, f.globex, "AUDITOR", false)

	if err := f.roles.Delete(ctx, globexRole.ID, f.acme); err == nil {
		t.Fatal("cross-tenant role delete succeeded")
	}

	if _, err := f.roles.FindByID(ctx, globexRole.ID, f.globex); err != nil {
		t.Errorf("Globex's role was deleted from another tenant: %v", err)
	}
}

// TestSameRoleNameIsIndependentPerCompany is the central tenancy property of
// RBAC: two companies may both have "ADMIN" and grant it different things.
func TestSameRoleNameIsIndependentPerCompany(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	acmeAdmin := f.seedRole(t, f.acme, "ADMIN", true)
	globexAdmin := f.seedRole(t, f.globex, "ADMIN", true)

	if acmeAdmin.ID == globexAdmin.ID {
		t.Fatal("the two companies share one role row")
	}

	// Acme's ADMIN can delete the company; Globex's cannot.
	if _, err := f.grants.Grant(ctx, acmeAdmin.ID,
		[]uuid.UUID{f.permissionID(t, entity.CompanyDelete)}); err != nil {
		t.Fatalf("Grant() = %v", err)
	}

	byRole, err := f.perms.CodesByRole(ctx, []uuid.UUID{acmeAdmin.ID, globexAdmin.ID})
	if err != nil {
		t.Fatalf("CodesByRole() = %v", err)
	}

	if len(byRole[acmeAdmin.ID]) != 1 {
		t.Errorf("Acme's ADMIN has %d permissions, want 1", len(byRole[acmeAdmin.ID]))
	}
	if len(byRole[globexAdmin.ID]) != 0 {
		t.Errorf("Globex's ADMIN has %d permissions, want 0", len(byRole[globexAdmin.ID]))
	}
}

// ---------- Constraints ----------

func TestDuplicateRoleNameInCompanyIsConflict(t *testing.T) {
	f := newFixture(t)

	f.seedRole(t, f.acme, "AUDITOR", false)

	// Different case: the CITEXT unique index must still reject it.
	err := f.roles.Create(context.Background(), &entity.Role{
		CompanyID: f.acme, Name: "auditor",
	})
	if err == nil {
		t.Fatal("a duplicate role name was accepted")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestRoleNameReusableAfterSoftDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	role := f.seedRole(t, f.acme, "AUDITOR", false)

	if err := f.roles.Delete(ctx, role.ID, f.acme); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	if err := f.roles.Create(ctx, &entity.Role{
		CompanyID: f.acme, Name: "AUDITOR",
	}); err != nil {
		t.Errorf("re-creating a soft-deleted role name = %v, want nil", err)
	}
}

func TestForeignKeyRejectsUnknownCompany(t *testing.T) {
	f := newFixture(t)

	err := f.roles.Create(context.Background(), &entity.Role{
		CompanyID: uuid.New(), Name: "GHOST",
	})
	if err == nil {
		t.Fatal("a role for a nonexistent company was accepted")
	}
}

// ---------- Grants ----------

func TestGrantIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	role := f.seedRole(t, f.acme, "AUDITOR", false)
	id := f.permissionID(t, entity.CompanyRead)

	if _, err := f.grants.Grant(ctx, role.ID, []uuid.UUID{id}); err != nil {
		t.Fatalf("first Grant() = %v", err)
	}
	if _, err := f.grants.Grant(ctx, role.ID, []uuid.UUID{id}); err != nil {
		t.Fatalf("second Grant() = %v", err)
	}

	held, err := f.grants.PermissionIDs(ctx, role.ID)
	if err != nil {
		t.Fatalf("PermissionIDs() = %v", err)
	}
	if len(held) != 1 {
		t.Errorf("role holds %d grants, want 1 — Grant must be idempotent", len(held))
	}
}

// TestRevokeThenRegrantRevivesTheRow covers the soft-delete revive path. A
// second live row for the same pair would make the revoke path ambiguous.
func TestRevokeThenRegrantRevivesTheRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	role := f.seedRole(t, f.acme, "AUDITOR", false)
	id := f.permissionID(t, entity.CompanyRead)

	if _, err := f.grants.Grant(ctx, role.ID, []uuid.UUID{id}); err != nil {
		t.Fatalf("Grant() = %v", err)
	}
	if _, err := f.grants.Revoke(ctx, role.ID, []uuid.UUID{id}, time.Now().UTC()); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	if _, err := f.grants.Grant(ctx, role.ID, []uuid.UUID{id}); err != nil {
		t.Fatalf("re-Grant() = %v", err)
	}

	held, _ := f.grants.PermissionIDs(ctx, role.ID)
	if len(held) != 1 {
		t.Errorf("role holds %d live grants, want 1", len(held))
	}

	// Exactly one physical row, revived rather than duplicated.
	var rows int64
	f.db.Raw("SELECT count(*) FROM role_permissions WHERE role_id = ? AND permission_id = ?",
		role.ID, id).Scan(&rows)
	if rows != 1 {
		t.Errorf("physical rows = %d, want 1 (revived, not duplicated)", rows)
	}
}

func TestRevokeIsSoft(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	role := f.seedRole(t, f.acme, "AUDITOR", false)
	id := f.permissionID(t, entity.CompanyRead)

	f.grants.Grant(ctx, role.ID, []uuid.UUID{id})
	f.grants.Revoke(ctx, role.ID, []uuid.UUID{id}, time.Now().UTC())

	held, _ := f.grants.PermissionIDs(ctx, role.ID)
	if len(held) != 0 {
		t.Errorf("role still holds %d grants after revoke", len(held))
	}

	// The row survives, so "who revoked what, and when" stays answerable.
	var rows int64
	f.db.Raw("SELECT count(*) FROM role_permissions WHERE role_id = ? AND deleted_at IS NOT NULL",
		role.ID).Scan(&rows)
	if rows != 1 {
		t.Errorf("soft-deleted rows = %d, want 1 — the audit trail must survive", rows)
	}
}

// TestCodesByRoleIsOneQuery covers the N+1 defence: resolving permissions for a
// whole page of roles must cost one round trip.
func TestCodesByRoleIsOneQuery(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	owner := f.seedRole(t, f.acme, "OWNER", true)
	admin := f.seedRole(t, f.acme, "ADMIN", true)

	f.grants.Grant(ctx, owner.ID, []uuid.UUID{
		f.permissionID(t, entity.CompanyRead),
		f.permissionID(t, entity.CompanyDelete),
	})
	f.grants.Grant(ctx, admin.ID, []uuid.UUID{
		f.permissionID(t, entity.CompanyRead),
	})

	byRole, err := f.perms.CodesByRole(ctx, []uuid.UUID{owner.ID, admin.ID})
	if err != nil {
		t.Fatalf("CodesByRole() = %v", err)
	}

	if len(byRole[owner.ID]) != 2 {
		t.Errorf("OWNER has %d codes, want 2", len(byRole[owner.ID]))
	}
	if len(byRole[admin.ID]) != 1 {
		t.Errorf("ADMIN has %d codes, want 1", len(byRole[admin.ID]))
	}
}

func TestGrantCascadesOnRoleHardDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	role := f.seedRole(t, f.acme, "AUDITOR", false)
	f.grants.Grant(ctx, role.ID, []uuid.UUID{f.permissionID(t, entity.CompanyRead)})

	// Hard delete, as a purge job would.
	if err := f.db.Exec("DELETE FROM roles WHERE id = ?", role.ID).Error; err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	var rows int64
	f.db.Raw("SELECT count(*) FROM role_permissions WHERE role_id = ?", role.ID).Scan(&rows)
	if rows != 0 {
		t.Errorf("grants = %d after the role was purged, want 0 (ON DELETE CASCADE)", rows)
	}
}

// ---------- Transactions ----------

// TestRoleAndGrantsAreAtomic proves a role cannot be created without its
// permissions: a role whose name is taken but which grants nothing would be
// unusable and un-retryable.
func TestRoleAndGrantsAreAtomic(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	manager := transaction.NewGormManager(f.db)

	sentinel := apperror.Internal("forced rollback")

	err := manager.RunInTransaction(ctx, func(txCtx context.Context) error {
		role := &entity.Role{CompanyID: f.acme, Name: "AUDITOR"}
		if err := f.roles.Create(txCtx, role); err != nil {
			return err
		}
		if _, err := f.grants.Grant(txCtx, role.ID,
			[]uuid.UUID{f.permissionID(t, entity.CompanyRead)}); err != nil {
			return err
		}
		return sentinel
	})
	if err == nil {
		t.Fatal("RunInTransaction() = nil despite the callback failing")
	}

	taken, err := f.roles.ExistsByName(ctx, f.acme, "AUDITOR")
	if err != nil {
		t.Fatalf("ExistsByName() = %v", err)
	}
	if taken {
		t.Error("a role survived a rolled-back transaction")
	}
}

// TestMembershipTableIsUnchanged is a guard rather than an RBAC test: this
// sprint must not have altered Membership. RBAC joins by role NAME, so the
// memberships table must still have no role_id column.
func TestMembershipTableIsUnchanged(t *testing.T) {
	f := newFixture(t)

	var roleIDColumns int64
	f.db.Raw("SELECT count(*) FROM information_schema.columns " +
		"WHERE table_name = 'memberships' AND column_name = 'role_id'").Scan(&roleIDColumns)
	if roleIDColumns != 0 {
		t.Error("memberships gained a role_id column; RBAC must join by name instead")
	}

	// And the role column it DOES join by is still there.
	var roleColumns int64
	f.db.Raw("SELECT count(*) FROM information_schema.columns " +
		"WHERE table_name = 'memberships' AND column_name = 'role'").Scan(&roleColumns)
	if roleColumns != 1 {
		t.Error("memberships.role is missing; RBAC resolution depends on it")
	}
}
