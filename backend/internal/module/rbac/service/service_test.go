package service

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

type harness struct {
	roleSvc       *RoleService
	permissionSvc *PermissionService
	evaluator     *Evaluator
	provisioner   *Provisioner

	roles       *fakeRoleRepo
	permissions *fakePermissionRepo
	grants      *fakeGrantRepo
	tx          *fakeTxManager
	events      *fakeEventPublisher
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	grants := newFakeGrantRepo()
	roles := newFakeRoleRepo()
	permissions := newFakePermissionRepo(grants)
	tx := &fakeTxManager{roles: roles, grants: grants}
	events := &fakeEventPublisher{}
	clock := adapterclock.NewFakeAt("2026-07-24T10:00:00Z")

	provisioner := NewProvisioner(roles, permissions, grants, tx, clock)
	evaluator := NewEvaluator(roles, permissions, provisioner)

	return &harness{
		roleSvc: NewRoleService(
			roles, permissions, grants, provisioner, clock, tx, events),
		permissionSvc: NewPermissionService(permissions, evaluator),
		evaluator:     evaluator,
		provisioner:   provisioner,
		roles:         roles,
		permissions:   permissions,
		grants:        grants,
		tx:            tx,
		events:        events,
	}
}

// scoped builds a context carrying a principal, an active company and a role
// name, standing in for auth + company middleware.
func scoped(userID, companyID uuid.UUID, roleName string) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, uuid.New(), roleName)
	return appcontext.Into(context.Background(), rc)
}

// ---------- Provisioning ----------

func TestProvisionCreatesThreeSystemRoles(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	if err := h.provisioner.EnsureSystemRoles(context.Background(), companyID); err != nil {
		t.Fatalf("EnsureSystemRoles() = %v", err)
	}

	for _, name := range entity.SystemRoleNames() {
		role, err := h.roles.FindByName(context.Background(), companyID, name)
		if err != nil {
			t.Fatalf("role %s was not provisioned: %v", name, err)
		}
		if !role.IsSystem {
			t.Errorf("role %s is not marked as a system role", name)
		}
	}
}

// TestProvisionIsIdempotent matters because it runs on the read path: a second
// call must not duplicate roles or reset their permissions.
func TestProvisionIsIdempotent(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := h.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	if got := h.roles.count(); got != 3 {
		t.Errorf("role count = %d, want 3", got)
	}
}

// TestProvisionDoesNotRepairEditedRoles is a security property: silently
// re-adding a permission an administrator deliberately revoked would be a
// regression disguised as a fix.
func TestProvisionDoesNotRepairEditedRoles(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := context.Background()

	if err := h.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		t.Fatalf("EnsureSystemRoles() = %v", err)
	}

	admin, _ := h.roles.FindByName(ctx, companyID, entity.SystemRoleAdmin)

	// An administrator revokes membership.invite from ADMIN.
	if _, err := h.grants.Revoke(ctx, admin.ID,
		[]uuid.UUID{h.permissions.idFor(entity.MembershipInvite)}, fixedNow()); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}

	// Re-provisioning must not put it back.
	if err := h.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		t.Fatalf("EnsureSystemRoles() = %v", err)
	}

	set, err := h.evaluator.ResolveSet(ctx, companyID, entity.SystemRoleAdmin)
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}
	if set.Has(entity.MembershipInvite) {
		t.Error("provisioning restored a deliberately revoked permission")
	}
}

// ---------- Permission evaluation ----------

func TestOwnerHasEveryPermission(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	set, err := h.evaluator.ResolveSet(context.Background(), companyID, entity.SystemRoleOwner)
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}

	for _, code := range entity.PermissionCatalogue() {
		if !set.Has(code) {
			t.Errorf("OWNER lacks %s", code)
		}
	}
}

// TestAdminIsOperationalNotStructural pins the boundary between ADMIN and
// OWNER: an admin who could edit roles could grant themselves anything, which
// would make the distinction cosmetic.
func TestAdminIsOperationalNotStructural(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	set, err := h.evaluator.ResolveSet(context.Background(), companyID, entity.SystemRoleAdmin)
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}

	for _, code := range []entity.Code{
		entity.CompanyRead, entity.CompanyUpdate,
		entity.MembershipRead, entity.MembershipInvite, entity.MembershipRemove,
		entity.RoleRead, entity.PermissionRead,
	} {
		if !set.Has(code) {
			t.Errorf("ADMIN lacks operational permission %s", code)
		}
	}

	for _, code := range []entity.Code{
		entity.CompanyDelete,
		entity.RoleCreate, entity.RoleUpdate, entity.RoleDelete,
		entity.RoleAssignPermissions,
	} {
		if set.Has(code) {
			t.Errorf("ADMIN holds structural permission %s; it should not", code)
		}
	}
}

func TestStaffIsReadOnly(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	set, err := h.evaluator.ResolveSet(context.Background(), companyID, entity.SystemRoleStaff)
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}

	for _, code := range []entity.Code{entity.CompanyRead, entity.MembershipRead} {
		if !set.Has(code) {
			t.Errorf("STAFF lacks %s", code)
		}
	}

	for _, code := range []entity.Code{
		entity.CompanyUpdate, entity.CompanyDelete,
		entity.MembershipInvite, entity.MembershipRemove,
		entity.RoleCreate, entity.RoleDelete, entity.RoleAssignPermissions,
	} {
		if set.Has(code) {
			t.Errorf("STAFF holds write permission %s", code)
		}
	}
}

// TestEvaluatorFailsClosed is the single most important property of the
// evaluator: every miss must deny, never fall back to a permissive default.
func TestEvaluatorFailsClosed(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	tests := map[string]string{
		"unknown custom role": "NO_SUCH_ROLE",
		"empty role name":     "",
	}

	for name, roleName := range tests {
		t.Run(name, func(t *testing.T) {
			set, err := h.evaluator.ResolveSet(context.Background(), companyID, roleName)
			if err != nil {
				t.Fatalf("ResolveSet() = %v", err)
			}
			if set.Len() != 0 {
				t.Errorf("resolved %d permissions, want 0 — the evaluator must fail closed",
					set.Len())
			}
		})
	}
}

// TestEvaluatorProvisionsOnFirstUse covers a company created before this sprint
// existed: without the retry its OWNER would be denied everything on their
// first request and succeed on their second.
func TestEvaluatorProvisionsOnFirstUse(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	if got := h.roles.count(); got != 0 {
		t.Fatalf("test setup: %d roles already exist", got)
	}

	set, err := h.evaluator.ResolveSet(context.Background(), companyID, entity.SystemRoleOwner)
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}
	if set.Len() == 0 {
		t.Fatal("the evaluator did not provision on first use")
	}
}

// TestCustomRoleIsNotProvisioned: only system names trigger provisioning, since
// re-provisioning would not create a custom role anyway.
func TestCustomRoleIsNotProvisioned(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()

	set, err := h.evaluator.ResolveSet(context.Background(), companyID, "AUDITOR")
	if err != nil {
		t.Fatalf("ResolveSet() = %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("a nonexistent custom role resolved %d permissions", set.Len())
	}
	if h.roles.count() != 0 {
		t.Error("resolving a custom role triggered provisioning")
	}
}

// ---------- Cross-company isolation ----------

// twoCompanies provisions two tenants and diverges their ADMIN definitions.
func (h *harness) twoCompanies(t *testing.T) (acme, globex uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	acme, globex = uuid.New(), uuid.New()

	for _, id := range []uuid.UUID{acme, globex} {
		if err := h.provisioner.EnsureSystemRoles(ctx, id); err != nil {
			t.Fatalf("provisioning: %v", err)
		}
	}
	return acme, globex
}

// TestPermissionsAreEvaluatedPerCompany is the headline tenancy guarantee for
// RBAC: the same role name resolves differently in different companies.
func TestPermissionsAreEvaluatedPerCompany(t *testing.T) {
	h := newHarness(t)
	acme, globex := h.twoCompanies(t)
	ctx := context.Background()

	// Acme strips membership.invite from its ADMIN. Globex does not.
	acmeAdmin, _ := h.roles.FindByName(ctx, acme, entity.SystemRoleAdmin)
	if _, err := h.grants.Revoke(ctx, acmeAdmin.ID,
		[]uuid.UUID{h.permissions.idFor(entity.MembershipInvite)}, fixedNow()); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}

	acmeSet, _ := h.evaluator.ResolveSet(ctx, acme, entity.SystemRoleAdmin)
	globexSet, _ := h.evaluator.ResolveSet(ctx, globex, entity.SystemRoleAdmin)

	if acmeSet.Has(entity.MembershipInvite) {
		t.Error("Acme's ADMIN kept a revoked permission")
	}
	if !globexSet.Has(entity.MembershipInvite) {
		t.Error("Globex's ADMIN lost a permission because of a change in another company")
	}
}

func TestCannotReadAnotherCompanysRole(t *testing.T) {
	h := newHarness(t)
	acme, globex := h.twoCompanies(t)
	ctx := context.Background()

	globexAdmin, _ := h.roles.FindByName(ctx, globex, entity.SystemRoleAdmin)

	// Knowing the id is not enough: scoped to Acme, it does not exist.
	_, err := h.roles.FindByID(ctx, globexAdmin.ID, acme)
	if err == nil {
		t.Fatal("cross-tenant role read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}
}

func TestCannotUpdateOrDeleteAnotherCompanysRole(t *testing.T) {
	h := newHarness(t)
	acme, globex := h.twoCompanies(t)
	ctx := context.Background()

	// Give Globex a deletable custom role.
	globexCtx := scoped(uuid.New(), globex, entity.SystemRoleOwner)
	custom, err := h.roleSvc.Create(globexCtx, dto.CreateRoleRequest{Name: "AUDITOR"})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	acmeCtx := scoped(uuid.New(), acme, entity.SystemRoleOwner)
	description := "hijacked"

	if _, err := h.roleSvc.Update(acmeCtx, custom.ID,
		dto.UpdateRoleRequest{Description: &description}); err == nil {
		t.Error("cross-tenant role update succeeded")
	}

	if err := h.roleSvc.Delete(acmeCtx, custom.ID); err == nil {
		t.Error("cross-tenant role delete succeeded")
	}

	if _, err := h.roles.FindByID(ctx, custom.ID, globex); err != nil {
		t.Errorf("Globex's role was affected: %v", err)
	}
}

// TestCannotAssignPermissionsAcrossCompanies is the escalation path that
// matters most: granting yourself permissions on another tenant's role.
func TestCannotAssignPermissionsAcrossCompanies(t *testing.T) {
	h := newHarness(t)
	acme, globex := h.twoCompanies(t)
	ctx := context.Background()

	globexStaff, _ := h.roles.FindByName(ctx, globex, entity.SystemRoleStaff)

	acmeCtx := scoped(uuid.New(), acme, entity.SystemRoleOwner)

	_, err := h.roleSvc.SetPermissions(acmeCtx, globexStaff.ID,
		dto.SetRolePermissionsRequest{Permissions: []string{entity.CompanyDelete.String()}})
	if err == nil {
		t.Fatal("assigning permissions to another company's role succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}

	// Globex's STAFF must be unchanged.
	set, _ := h.evaluator.ResolveSet(ctx, globex, entity.SystemRoleStaff)
	if set.Has(entity.CompanyDelete) {
		t.Error("another company escalated Globex's STAFF role")
	}
}

// ---------- Owner protection ----------

// TestOwnerRoleCannotBeWeakened: ownership is the recovery path for every other
// authorisation mistake, so it must not be removable.
func TestOwnerRoleCannotBeWeakened(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	if err := h.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	owner, _ := h.roles.FindByName(ctx, companyID, entity.SystemRoleOwner)

	_, err := h.roleSvc.SetPermissions(ctx, owner.ID,
		dto.SetRolePermissionsRequest{Permissions: []string{entity.CompanyRead.String()}})
	if err == nil {
		t.Fatal("the OWNER role's permissions were changed")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	// It still holds everything.
	set, _ := h.evaluator.ResolveSet(ctx, companyID, entity.SystemRoleOwner)
	for _, code := range entity.PermissionCatalogue() {
		if !set.Has(code) {
			t.Errorf("OWNER lost %s despite the rejection", code)
		}
	}
}

func TestSystemRolesCannotBeDeleted(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	if err := h.provisioner.EnsureSystemRoles(ctx, companyID); err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	for _, name := range entity.SystemRoleNames() {
		role, _ := h.roles.FindByName(ctx, companyID, name)

		err := h.roleSvc.Delete(ctx, role.ID)
		if err == nil {
			t.Errorf("system role %s was deleted", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeConflict {
			t.Errorf("%s: code = %s, want CONFLICT", name, code)
		}
	}

	if got := h.roles.count(); got != 3 {
		t.Errorf("role count = %d, want the three system roles intact", got)
	}
}

// TestCannotCreateRoleShadowingASystemName stops a caller redefining what
// memberships resolve against.
func TestCannotCreateRoleShadowingASystemName(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	for _, name := range []string{"OWNER", "admin", " Staff "} {
		_, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{Name: name})
		if err == nil {
			t.Errorf("created a role named %q, shadowing a system role", name)
			continue
		}
		if code := apperror.From(err).Code; code != apperror.CodeValidation {
			t.Errorf("%q: code = %s, want VALIDATION_ERROR", name, code)
		}
	}
}

// ---------- Role CRUD ----------

func TestCreateCustomRoleWithPermissions(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	got, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "auditor",
		Description: "Read-only reviewer",
		Permissions: []string{
			entity.CompanyRead.String(), entity.MembershipRead.String(),
		},
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if got.Name != "AUDITOR" {
		t.Errorf("name = %q, want it normalised to AUDITOR", got.Name)
	}
	if got.IsSystem {
		t.Error("a caller-created role was marked as a system role")
	}

	want := []string{"company.read", "membership.read"}
	if !equalStrings(got.Permissions, want) {
		t.Errorf("permissions = %v, want %v", got.Permissions, want)
	}
	if !h.events.has(entity.EventRoleCreated) {
		t.Errorf("RoleCreated not published; got %v", h.events.names())
	}
	if !h.events.has(entity.EventPermissionAssigned) {
		t.Errorf("PermissionAssigned not published; got %v", h.events.names())
	}
}

func TestCreateRoleRejectsUnknownPermission(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New(), entity.SystemRoleOwner)

	_, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "AUDITOR",
		Permissions: []string{"warehouse.destroy"},
	})
	if err == nil {
		t.Fatal("Create() = nil for an unknown permission code")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
}

// TestCreateRoleRollsBack proves the role and its grants are atomic: a role
// whose name is taken but which holds no permissions would be unusable and
// un-retryable.
func TestCreateRoleRollsBack(t *testing.T) {
	h := newHarness(t)
	ctx := scoped(uuid.New(), uuid.New(), entity.SystemRoleOwner)

	h.roles.fail("Create", errors.New("role store unavailable"))

	if _, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{Name: "AUDITOR"}); err == nil {
		t.Fatal("Create() = nil despite the store failing")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}
	if h.roles.count() != 0 {
		t.Error("a role survived a rolled-back create")
	}
}

func TestDeleteCustomRole(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	created, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{Name: "AUDITOR"})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if err := h.roleSvc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if !h.events.has(entity.EventRoleDeleted) {
		t.Errorf("RoleDeleted not published; got %v", h.events.names())
	}
	if _, err := h.roles.FindByID(ctx, created.ID, companyID); err == nil {
		t.Error("the role survived deletion")
	}
}

// ---------- Permission assignment ----------

// TestSetPermissionsComputesDelta checks the audit trail records what CHANGED,
// not just the final state — the difference between a log that can answer "when
// did ADMIN lose company.update?" and one that cannot.
func TestSetPermissionsComputesDelta(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	created, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "AUDITOR",
		Permissions: []string{entity.CompanyRead.String(), entity.MembershipRead.String()},
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	h.events.reset()

	// Keep company.read, drop membership.read, add role.read.
	got, err := h.roleSvc.SetPermissions(ctx, created.ID, dto.SetRolePermissionsRequest{
		Permissions: []string{entity.CompanyRead.String(), entity.RoleRead.String()},
	})
	if err != nil {
		t.Fatalf("SetPermissions() = %v", err)
	}

	want := []string{"company.read", "role.read"}
	if !equalStrings(got.Permissions, want) {
		t.Errorf("permissions = %v, want %v", got.Permissions, want)
	}

	assigned, ok := h.events.find(entity.EventPermissionAssigned)
	if !ok {
		t.Fatal("PermissionAssigned was not published")
	}
	if codes, _ := assigned.Attributes["permissions"].([]string); !equalStrings(
		codes, []string{"role.read"}) {
		t.Errorf("assigned = %v, want only the added permission", assigned.Attributes["permissions"])
	}

	revoked, ok := h.events.find(entity.EventPermissionRevoked)
	if !ok {
		t.Fatal("PermissionRevoked was not published")
	}
	if codes, _ := revoked.Attributes["permissions"].([]string); !equalStrings(
		codes, []string{"membership.read"}) {
		t.Errorf("revoked = %v, want only the removed permission", revoked.Attributes["permissions"])
	}
}

// TestSetPermissionsIsIdempotent: re-sending the same set changes nothing and
// emits no spurious audit noise.
func TestSetPermissionsIsIdempotent(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	created, _ := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "AUDITOR",
		Permissions: []string{entity.CompanyRead.String()},
	})

	h.events.reset()

	if _, err := h.roleSvc.SetPermissions(ctx, created.ID, dto.SetRolePermissionsRequest{
		Permissions: []string{entity.CompanyRead.String()},
	}); err != nil {
		t.Fatalf("SetPermissions() = %v", err)
	}

	if h.events.has(entity.EventPermissionAssigned) || h.events.has(entity.EventPermissionRevoked) {
		t.Errorf("a no-op change emitted audit events: %v", h.events.names())
	}
}

// TestSetPermissionsEmptyRevokesAll: an explicit empty array is meaningful.
func TestSetPermissionsEmptyRevokesAll(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	created, _ := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "AUDITOR",
		Permissions: []string{entity.CompanyRead.String(), entity.RoleRead.String()},
	})

	got, err := h.roleSvc.SetPermissions(ctx, created.ID,
		dto.SetRolePermissionsRequest{Permissions: []string{}})
	if err != nil {
		t.Fatalf("SetPermissions() = %v", err)
	}
	if len(got.Permissions) != 0 {
		t.Errorf("permissions = %v, want none", got.Permissions)
	}
}

// TestRevokedPermissionCanBeRegranted covers the soft-delete revive path.
func TestRevokedPermissionCanBeRegranted(t *testing.T) {
	h := newHarness(t)
	companyID := uuid.New()
	ctx := scoped(uuid.New(), companyID, entity.SystemRoleOwner)

	created, _ := h.roleSvc.Create(ctx, dto.CreateRoleRequest{
		Name:        "AUDITOR",
		Permissions: []string{entity.CompanyRead.String()},
	})

	if _, err := h.roleSvc.SetPermissions(ctx, created.ID,
		dto.SetRolePermissionsRequest{Permissions: []string{}}); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := h.roleSvc.SetPermissions(ctx, created.ID,
		dto.SetRolePermissionsRequest{Permissions: []string{entity.CompanyRead.String()}})
	if err != nil {
		t.Fatalf("re-grant: %v", err)
	}
	if !equalStrings(got.Permissions, []string{"company.read"}) {
		t.Errorf("permissions = %v, want company.read restored", got.Permissions)
	}
}

// ---------- Requirements ----------

func TestRoleOperationsRequireTenant(t *testing.T) {
	h := newHarness(t)

	rc := appcontext.New("test", zapNop())
	userID := uuid.New()
	rc.WithTenant(nil, &userID, "")
	ctx := appcontext.Into(context.Background(), rc)

	if _, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{Name: "AUDITOR"}); err == nil {
		t.Error("Create() = nil with no active company")
	} else if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}

	if _, err := h.permissionSvc.List(ctx, dto.ListPermissionsQuery{}); err == nil {
		t.Error("permission List() = nil with no active company")
	}
}

// ---------- Audit ----------

func TestEventsCarryTenantAndActor(t *testing.T) {
	h := newHarness(t)
	companyID, actorID := uuid.New(), uuid.New()
	ctx := scoped(actorID, companyID, entity.SystemRoleOwner)

	if _, err := h.roleSvc.Create(ctx, dto.CreateRoleRequest{Name: "AUDITOR"}); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	event, ok := h.events.find(entity.EventRoleCreated)
	if !ok {
		t.Fatal("RoleCreated was not published")
	}
	if event.CompanyID != companyID {
		t.Errorf("event company = %s, want %s", event.CompanyID, companyID)
	}
	if event.ActorID != actorID {
		t.Errorf("event actor = %s, want %s", event.ActorID, actorID)
	}
}

// equalStrings compares two slices ignoring order.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	a := append([]string(nil), got...)
	b := append([]string(nil), want...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
