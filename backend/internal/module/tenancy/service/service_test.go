package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/dto"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	adapterclock "github.com/batokhehe/wms-saas/backend/internal/shared/adapter/clock"
	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
}

type harness struct {
	companySvc    *CompanyService
	membershipSvc *MembershipService
	resolver      *ContextResolver

	companies   *fakeCompanyRepo
	memberships *fakeMembershipRepo
	tx          *fakeTxManager
	events      *fakeEventPublisher
	clock       *adapterclock.Fake

	// directory maps email -> user id for the invite flow.
	directory map[string]uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	memberships := newFakeMembershipRepo()
	companies := newFakeCompanyRepo(memberships)
	tx := &fakeTxManager{companies: companies, memberships: memberships}
	events := &fakeEventPublisher{}
	clock := adapterclock.NewFakeAt("2026-07-23T10:00:00Z")

	h := &harness{
		companies:   companies,
		memberships: memberships,
		tx:          tx,
		events:      events,
		clock:       clock,
		directory:   map[string]uuid.UUID{},
	}

	directory := UserDirectoryFunc(func(_ context.Context, email string) (uuid.UUID, error) {
		if id, ok := h.directory[email]; ok {
			return id, nil
		}
		return uuid.Nil, apperror.NotFound("user not found").WithOp("fake.directory")
	})

	h.companySvc = NewCompanyService(companies, memberships, clock, tx, events)
	h.membershipSvc = NewMembershipService(memberships, companies, directory, clock, tx, events)
	h.resolver = NewContextResolver(memberships, companies)

	return h
}

// authed builds a context carrying an authenticated principal but NO company,
// standing in for what the auth middleware alone produces.
func authed(userID uuid.UUID) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	return appcontext.Into(context.Background(), rc)
}

// scoped builds a context carrying both a principal and an active company,
// standing in for auth + company middleware.
func scoped(userID, companyID, membershipID uuid.UUID, role entity.Role) context.Context {
	rc := appcontext.New("test-request", zapNop())
	rc.WithTenant(nil, &userID, "")
	rc.WithCompany(companyID, membershipID, string(role))
	return appcontext.Into(context.Background(), rc)
}

// ---------- Company: create (the register flow tail) ----------

func TestCreateCompanyMakesCallerOwner(t *testing.T) {
	h := newHarness(t)
	userID := uuid.New()

	got, err := h.companySvc.Create(authed(userID), dto.CreateCompanyRequest{
		Code: "acme", Name: "Acme Logistics", Email: "ops@acme.test",
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// The code is normalised on the way in, so the stored form is canonical.
	if got.Company.Code != "ACME" {
		t.Errorf("code = %q, want it normalised to ACME", got.Company.Code)
	}
	if got.Company.Status != string(entity.CompanyActive) {
		t.Errorf("status = %q, want ACTIVE", got.Company.Status)
	}
	if got.Role != string(entity.RoleOwner) {
		t.Errorf("role = %q, want OWNER", got.Role)
	}
	if got.MembershipID == uuid.Nil {
		t.Error("no membership was created")
	}
	if !h.events.has(entity.EventCompanyCreated) {
		t.Errorf("CompanyCreated not published; got %v", h.events.names())
	}
}

// TestCreateCompanyOwnerMembershipIsActive: a PENDING owner could not reach the
// company they just created.
func TestCreateCompanyOwnerMembershipIsActive(t *testing.T) {
	h := newHarness(t)
	userID := uuid.New()

	got, err := h.companySvc.Create(authed(userID), dto.CreateCompanyRequest{
		Code: "acme", Name: "Acme",
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	membership := h.memberships.findActive(userID, got.Company.ID)
	if membership == nil {
		t.Fatal("the founder has no ACTIVE membership")
	}
	if membership.JoinedAt == nil {
		t.Error("joined_at was not stamped on the founder's membership")
	}
	if membership.InvitedBy != nil {
		t.Error("invited_by should be nil for a founder — nobody invited them")
	}
}

// TestCreateCompanyRollsBack proves onboarding is atomic. A company without a
// membership is unreachable by ANYONE, including its creator, and its code is
// permanently consumed.
func TestCreateCompanyRollsBack(t *testing.T) {
	h := newHarness(t)
	h.memberships.fail("Create", errors.New("membership store unavailable"))

	_, err := h.companySvc.Create(authed(uuid.New()), dto.CreateCompanyRequest{
		Code: "acme", Name: "Acme",
	})
	if err == nil {
		t.Fatal("Create() = nil despite the membership store failing")
	}
	if h.tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", h.tx.rollbacks)
	}

	// The orphaned company must be gone, so the code is free for a retry.
	taken, _ := h.companies.ExistsByCode(context.Background(), "ACME")
	if taken {
		t.Error("an orphaned company survived a rolled-back onboarding")
	}
}

func TestCreateCompanyRejectsDuplicateCode(t *testing.T) {
	h := newHarness(t)
	h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})

	// Different case: normalisation must still catch it.
	_, err := h.companySvc.Create(authed(uuid.New()), dto.CreateCompanyRequest{
		Code: "acme", Name: "Impostor",
	})
	if err == nil {
		t.Fatal("Create() = nil for a duplicate code")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}
}

func TestCreateCompanyRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)

	tests := map[string]dto.CreateCompanyRequest{
		"reserved code":  {Code: "ADMIN", Name: "Acme"},
		"code too short": {Code: "A", Name: "Acme"},
		"blank name":     {Code: "ACME", Name: "   "},
	}

	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := h.companySvc.Create(authed(uuid.New()), req)
			if err == nil {
				t.Fatalf("Create(%+v) = nil, want a validation error", req)
			}
			if code := apperror.From(err).Code; code != apperror.CodeValidation {
				t.Errorf("code = %s, want VALIDATION_ERROR", code)
			}
		})
	}
}

func TestCreateCompanyRequiresAuthentication(t *testing.T) {
	h := newHarness(t)

	_, err := h.companySvc.Create(context.Background(), dto.CreateCompanyRequest{
		Code: "ACME", Name: "Acme",
	})
	if err == nil {
		t.Fatal("Create() = nil without a principal")
	}
	if code := apperror.From(err).Code; code != apperror.CodeUnauthorized {
		t.Errorf("code = %s, want UNAUTHORIZED", code)
	}
}

// ---------- Cross-company isolation ----------

// twoCompanies sets up two tenants with one member each, plus a third user who
// belongs to both. It is the fixture for every isolation test below.
func (h *harness) twoCompanies(t *testing.T) (
	acmeID, globexID, aliceID, bobID uuid.UUID,
) {
	t.Helper()

	acme := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	globex := h.companies.seed(entity.Company{Code: "GLOBEX", Name: "Globex"})

	alice := uuid.New()
	bob := uuid.New()

	h.memberships.seed(entity.Membership{
		CompanyID: acme.ID, UserID: alice, Role: entity.RoleOwner,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: globex.ID, UserID: bob, Role: entity.RoleOwner,
	})

	return acme.ID, globex.ID, alice, bob
}

// TestCannotReadAnotherCompany is the headline isolation guarantee.
func TestCannotReadAnotherCompany(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, _ := h.twoCompanies(t)

	// Alice can read her own.
	if _, err := h.companySvc.Get(authed(alice), acmeID); err != nil {
		t.Fatalf("Get(own company) = %v, want nil", err)
	}

	// She cannot read Globex, and the failure is NOT_FOUND rather than
	// FORBIDDEN — a 403 would confirm that a company with that id exists.
	_, err := h.companySvc.Get(authed(alice), globexID)
	if err == nil {
		t.Fatal("Get(another company) = nil; cross-tenant read succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND (FORBIDDEN would confirm existence)", code)
	}
}

func TestListOnlyReturnsOwnCompanies(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, _ := h.twoCompanies(t)

	page, err := h.companySvc.List(authed(alice), dto.ListCompaniesQuery{})
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	if len(page.Items) != 1 {
		t.Fatalf("List() returned %d companies, want 1", len(page.Items))
	}
	if page.Items[0].ID != acmeID {
		t.Errorf("List() returned %s, want %s", page.Items[0].ID, acmeID)
	}
	for _, item := range page.Items {
		if item.ID == globexID {
			t.Error("List() leaked another tenant's company")
		}
	}
}

func TestCannotUpdateOrDeleteAnotherCompany(t *testing.T) {
	h := newHarness(t)
	_, globexID, alice, _ := h.twoCompanies(t)

	name := "Hijacked"
	if _, err := h.companySvc.Update(authed(alice), globexID,
		dto.UpdateCompanyRequest{Name: &name}); err == nil {
		t.Error("Update(another company) succeeded")
	}

	if err := h.companySvc.Delete(authed(alice), globexID); err == nil {
		t.Error("Delete(another company) succeeded")
	}

	// Globex must be untouched.
	company, err := h.companies.FindByIDUnscoped(context.Background(), globexID)
	if err != nil {
		t.Fatalf("Globex was deleted: %v", err)
	}
	if company.Name != "Globex" {
		t.Errorf("Globex name = %q, want it unchanged", company.Name)
	}
}

// TestCannotListAnotherCompanysMembers checks the member list is bound to the
// ACTIVE company context, not to a client-supplied parameter.
func TestCannotListAnotherCompanysMembers(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, bob := h.twoCompanies(t)

	aliceMembership := h.memberships.findActive(alice, acmeID)

	page, err := h.membershipSvc.List(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		dto.ListMembershipsQuery{},
	)
	if err != nil {
		t.Fatalf("List() = %v", err)
	}

	for _, item := range page.Items {
		if item.CompanyID == globexID {
			t.Error("member list leaked another tenant's membership")
		}
		if item.UserID == bob {
			t.Error("member list leaked another tenant's user")
		}
	}
	if len(page.Items) != 1 {
		t.Errorf("List() returned %d members, want 1", len(page.Items))
	}
}

// TestCannotRemoveAnotherCompanysMember: knowing a membership id is not enough
// to act on it.
func TestCannotRemoveAnotherCompanysMember(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, bob := h.twoCompanies(t)

	bobMembership := h.memberships.findActive(bob, globexID)
	aliceMembership := h.memberships.findActive(alice, acmeID)

	err := h.membershipSvc.Remove(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		bobMembership.ID,
	)
	if err == nil {
		t.Fatal("removing another tenant's member succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", code)
	}

	// Bob is still a member of Globex.
	if h.memberships.findActive(bob, globexID) == nil {
		t.Error("Bob's membership was removed by a member of another company")
	}
}

// ---------- Company switching ----------

func TestSwitchToCompanyIAmAMemberOf(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, _ := h.twoCompanies(t)

	// Alice joins Globex too.
	h.memberships.seed(entity.Membership{
		CompanyID: globexID, UserID: alice, Role: entity.RoleStaff,
	})

	got, err := h.companySvc.Switch(authed(alice), dto.SwitchCompanyRequest{
		CompanyID: globexID,
	})
	if err != nil {
		t.Fatalf("Switch() = %v", err)
	}

	if got.Company.ID != globexID {
		t.Errorf("switched to %s, want %s", got.Company.ID, globexID)
	}
	// Role is a property of the RELATIONSHIP: Alice is OWNER at Acme and STAFF
	// at Globex, and switching must reflect that.
	if got.Role != string(entity.RoleStaff) {
		t.Errorf("role = %q, want STAFF at Globex", got.Role)
	}
	if !h.events.has(entity.EventCompanySwitched) {
		t.Errorf("CompanySwitched not published; got %v", h.events.names())
	}

	_ = acmeID
}

func TestSwitchToCompanyIAmNotAMemberOfIsForbidden(t *testing.T) {
	h := newHarness(t)
	_, globexID, alice, _ := h.twoCompanies(t)

	_, err := h.companySvc.Switch(authed(alice), dto.SwitchCompanyRequest{
		CompanyID: globexID,
	})
	if err == nil {
		t.Fatal("Switch() = nil for a company the caller cannot access")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// TestSwitchToUnknownCompanyLooksIdentical: the message must not distinguish
// "no such company" from "not a member", or any user could probe for tenants.
func TestSwitchToUnknownCompanyLooksIdentical(t *testing.T) {
	h := newHarness(t)
	_, globexID, alice, _ := h.twoCompanies(t)

	_, notMember := h.companySvc.Switch(authed(alice),
		dto.SwitchCompanyRequest{CompanyID: globexID})
	_, noSuchCompany := h.companySvc.Switch(authed(alice),
		dto.SwitchCompanyRequest{CompanyID: uuid.New()})

	if notMember == nil || noSuchCompany == nil {
		t.Fatal("both switches should have failed")
	}

	a, b := apperror.From(notMember), apperror.From(noSuchCompany)
	if a.Code != b.Code || a.Message != b.Message {
		t.Errorf("responses differ (%s/%q vs %s/%q) — this leaks tenant existence",
			a.Code, a.Message, b.Code, b.Message)
	}
}

func TestSwitchToSuspendedCompanyIsForbidden(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	suspended := h.companies.seed(entity.Company{
		Code: "SUSP", Name: "Suspended Co", Status: entity.CompanySuspended,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: suspended.ID, UserID: alice, Role: entity.RoleOwner,
	})

	_, err := h.companySvc.Switch(authed(alice), dto.SwitchCompanyRequest{
		CompanyID: suspended.ID,
	})
	if err == nil {
		t.Fatal("Switch() = nil for a suspended company")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// TestSwitchIgnoresPendingMembership: an unaccepted invitation is not access.
func TestSwitchIgnoresPendingMembership(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	company := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	h.memberships.seed(entity.Membership{
		CompanyID: company.ID, UserID: alice,
		Role: entity.RoleStaff, Status: entity.MembershipPending,
	})

	_, err := h.companySvc.Switch(authed(alice), dto.SwitchCompanyRequest{
		CompanyID: company.ID,
	})
	if err == nil {
		t.Fatal("Switch() = nil for a PENDING membership; an invitation is not access")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Current company ----------

func TestCurrentCompanyReflectsContext(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	membership := h.memberships.findActive(alice, acmeID)

	got, err := h.companySvc.Current(scoped(alice, acmeID, membership.ID, entity.RoleOwner))
	if err != nil {
		t.Fatalf("Current() = %v", err)
	}

	if got.Company.ID != acmeID {
		t.Errorf("company = %s, want %s", got.Company.ID, acmeID)
	}
	if got.Role != string(entity.RoleOwner) {
		t.Errorf("role = %q, want OWNER", got.Role)
	}
}

func TestCurrentCompanyRequiresTenant(t *testing.T) {
	h := newHarness(t)

	_, err := h.companySvc.Current(authed(uuid.New()))
	if err == nil {
		t.Fatal("Current() = nil with no active company")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Membership: invite ----------

func TestInviteCreatesPendingMembership(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	invitee := uuid.New()
	h.directory["new@acme.test"] = invitee

	aliceMembership := h.memberships.findActive(alice, acmeID)

	got, err := h.membershipSvc.Invite(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		dto.InviteMemberRequest{Email: "new@acme.test", Role: "STAFF"},
	)
	if err != nil {
		t.Fatalf("Invite() = %v", err)
	}

	// PENDING, not ACTIVE: an invitation reserves a seat, it does not grant
	// access.
	if got.Status != string(entity.MembershipPending) {
		t.Errorf("status = %q, want PENDING", got.Status)
	}
	if got.JoinedAt != nil {
		t.Error("joined_at must be nil until the invitation is accepted")
	}
	if got.InvitedBy == nil || *got.InvitedBy != alice {
		t.Errorf("invited_by = %v, want %s", got.InvitedBy, alice)
	}
	if !h.events.has(entity.EventMemberInvited) {
		t.Errorf("MemberInvited not published; got %v", h.events.names())
	}
}

// TestInvitedMemberCannotAccessYet is the security consequence of PENDING.
func TestInvitedMemberCannotAccessYet(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	invitee := uuid.New()
	h.directory["new@acme.test"] = invitee
	aliceMembership := h.memberships.findActive(alice, acmeID)

	if _, err := h.membershipSvc.Invite(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		dto.InviteMemberRequest{Email: "new@acme.test", Role: "STAFF"},
	); err != nil {
		t.Fatalf("Invite() = %v", err)
	}

	// The invitee cannot read the company they were invited to.
	if _, err := h.companySvc.Get(authed(invitee), acmeID); err == nil {
		t.Error("a PENDING member could read the company; an invitation is not access")
	}

	// Nor can they resolve it as a context.
	if _, err := h.resolver.Resolve(context.Background(), invitee, acmeID); err == nil {
		t.Error("a PENDING membership resolved a company context")
	}
}

func TestInviteRejectsOwnerRole(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	h.directory["new@acme.test"] = uuid.New()
	aliceMembership := h.memberships.findActive(alice, acmeID)

	_, err := h.membershipSvc.Invite(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		dto.InviteMemberRequest{Email: "new@acme.test", Role: "OWNER"},
	)
	if err == nil {
		t.Fatal("Invite(OWNER) = nil; ownership is a transfer, not an invitation")
	}
	if code := apperror.From(err).Code; code != apperror.CodeValidation {
		t.Errorf("code = %s, want VALIDATION_ERROR", code)
	}
}

// TestInviteHidesWhetherAccountExists: the failure must look the same whether
// the address is unregistered or already a member, or the endpoint becomes an
// account enumeration oracle.
func TestInviteHidesWhetherAccountExists(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)
	aliceMembership := h.memberships.findActive(alice, acmeID)
	ctx := scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner)

	// Unregistered address.
	_, unknown := h.membershipSvc.Invite(ctx,
		dto.InviteMemberRequest{Email: "nobody@acme.test", Role: "STAFF"})

	// Already a member (Alice herself).
	h.directory["alice@acme.test"] = alice
	_, existing := h.membershipSvc.Invite(ctx,
		dto.InviteMemberRequest{Email: "alice@acme.test", Role: "STAFF"})

	if unknown == nil || existing == nil {
		t.Fatal("both invitations should have failed")
	}

	a, b := apperror.From(unknown), apperror.From(existing)
	if a.Code != b.Code || a.Message != b.Message {
		t.Errorf("responses differ (%s/%q vs %s/%q) — this leaks account existence",
			a.Code, a.Message, b.Code, b.Message)
	}
}

func TestInviteRequiresCompanyContext(t *testing.T) {
	h := newHarness(t)
	h.directory["new@acme.test"] = uuid.New()

	_, err := h.membershipSvc.Invite(authed(uuid.New()),
		dto.InviteMemberRequest{Email: "new@acme.test", Role: "STAFF"})
	if err == nil {
		t.Fatal("Invite() = nil with no active company")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

// ---------- Membership: remove ----------

func TestRemoveMember(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	staff := uuid.New()
	staffMembership := h.memberships.seed(entity.Membership{
		CompanyID: acmeID, UserID: staff, Role: entity.RoleStaff,
	})
	aliceMembership := h.memberships.findActive(alice, acmeID)

	err := h.membershipSvc.Remove(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		staffMembership.ID,
	)
	if err != nil {
		t.Fatalf("Remove() = %v", err)
	}

	if h.memberships.findActive(staff, acmeID) != nil {
		t.Error("the member was not removed")
	}
	if !h.events.has(entity.EventMemberRemoved) {
		t.Errorf("MemberRemoved not published; got %v", h.events.names())
	}
}

// TestCannotRemoveLastOwner: removing the final owner leaves a company nobody
// can administer, and ownership can only be granted at creation.
func TestCannotRemoveLastOwner(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	aliceMembership := h.memberships.findActive(alice, acmeID)

	err := h.membershipSvc.Remove(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		aliceMembership.ID,
	)
	if err == nil {
		t.Fatal("removing the last owner succeeded")
	}
	if code := apperror.From(err).Code; code != apperror.CodeConflict {
		t.Errorf("code = %s, want CONFLICT", code)
	}

	if h.memberships.findActive(alice, acmeID) == nil {
		t.Error("the last owner was removed despite the error")
	}
}

// TestCanRemoveOwnerWhenAnotherExists is the counterpart: the rule protects the
// LAST owner, not every owner.
func TestCanRemoveOwnerWhenAnotherExists(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	second := uuid.New()
	secondMembership := h.memberships.seed(entity.Membership{
		CompanyID: acmeID, UserID: second, Role: entity.RoleOwner,
	})
	aliceMembership := h.memberships.findActive(alice, acmeID)

	err := h.membershipSvc.Remove(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		secondMembership.ID,
	)
	if err != nil {
		t.Errorf("Remove(one of two owners) = %v, want nil", err)
	}
}

// ---------- Mine (switcher menu) ----------

func TestMineListsEveryActiveCompany(t *testing.T) {
	h := newHarness(t)
	acmeID, globexID, alice, _ := h.twoCompanies(t)

	h.memberships.seed(entity.Membership{
		CompanyID: globexID, UserID: alice, Role: entity.RoleStaff,
	})

	got, err := h.membershipSvc.Mine(authed(alice))
	if err != nil {
		t.Fatalf("Mine() = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("Mine() returned %d memberships, want 2", len(got))
	}

	roles := map[uuid.UUID]string{}
	for _, m := range got {
		roles[m.Company.ID] = m.Role
	}
	if roles[acmeID] != string(entity.RoleOwner) {
		t.Errorf("role at Acme = %q, want OWNER", roles[acmeID])
	}
	if roles[globexID] != string(entity.RoleStaff) {
		t.Errorf("role at Globex = %q, want STAFF", roles[globexID])
	}
}

func TestMineExcludesPending(t *testing.T) {
	h := newHarness(t)
	_, globexID, alice, _ := h.twoCompanies(t)

	h.memberships.seed(entity.Membership{
		CompanyID: globexID, UserID: alice,
		Role: entity.RoleStaff, Status: entity.MembershipPending,
	})

	got, err := h.membershipSvc.Mine(authed(alice))
	if err != nil {
		t.Fatalf("Mine() = %v", err)
	}

	for _, m := range got {
		if m.Company.ID == globexID {
			t.Error("Mine() included a PENDING membership")
		}
	}
}

func TestMineReturnsEmptySliceNotNil(t *testing.T) {
	h := newHarness(t)

	got, err := h.membershipSvc.Mine(authed(uuid.New()))
	if err != nil {
		t.Fatalf("Mine() = %v", err)
	}
	if got == nil {
		t.Error("Mine() returned nil; it must be an empty slice so JSON emits []")
	}
}

// ---------- Audit events ----------

// TestEventsCarryNoPersonalData: audit records are forwarded to systems with
// different access controls, so they carry identifiers rather than emails.
func TestEventsCarryNoPersonalData(t *testing.T) {
	h := newHarness(t)
	acmeID, _, alice, _ := h.twoCompanies(t)

	h.directory["invitee@acme.test"] = uuid.New()
	aliceMembership := h.memberships.findActive(alice, acmeID)
	h.events.reset()

	if _, err := h.membershipSvc.Invite(
		scoped(alice, acmeID, aliceMembership.ID, entity.RoleOwner),
		dto.InviteMemberRequest{Email: "invitee@acme.test", Role: "STAFF"},
	); err != nil {
		t.Fatalf("Invite() = %v", err)
	}

	event, ok := h.events.find(entity.EventMemberInvited)
	if !ok {
		t.Fatal("MemberInvited was not published")
	}

	if event.CompanyID != acmeID {
		t.Errorf("event company = %s, want %s", event.CompanyID, acmeID)
	}
	if event.ActorID != alice {
		t.Errorf("event actor = %s, want the inviter %s", event.ActorID, alice)
	}

	for key, value := range event.Attributes {
		if rendered, ok := value.(string); ok && rendered == "invitee@acme.test" {
			t.Errorf("attribute %q contains the invitee's email address", key)
		}
	}
}
