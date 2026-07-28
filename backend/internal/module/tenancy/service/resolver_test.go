package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// TestResolveSingleMembershipNeedsNoHeader covers the overwhelmingly common
// case: one person, one company. Making them send a header for it would be
// pointless friction.
func TestResolveSingleMembershipNeedsNoHeader(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	company := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	membership := h.memberships.seed(entity.Membership{
		CompanyID: company.ID, UserID: alice, Role: entity.RoleOwner,
	})

	got, err := h.resolver.Resolve(context.Background(), alice, uuid.Nil)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	if got.CompanyID != company.ID {
		t.Errorf("company = %s, want %s", got.CompanyID, company.ID)
	}
	if got.MembershipID != membership.ID {
		t.Errorf("membership = %s, want %s", got.MembershipID, membership.ID)
	}
	if got.Role != string(entity.RoleOwner) {
		t.Errorf("role = %q, want OWNER", got.Role)
	}
}

// TestResolveRefusesToGuessBetweenCompanies is the most important test in this
// file.
//
// Auto-selecting for a multi-company user would mean a request without a header
// lands in whichever tenant happened to sort first — so a warehouse operator
// working for two clients could ship stock from the wrong one with no error
// raised. Ambiguity must be refused, not guessed.
func TestResolveRefusesToGuessBetweenCompanies(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	acme := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	globex := h.companies.seed(entity.Company{Code: "GLOBEX", Name: "Globex"})

	h.memberships.seed(entity.Membership{
		CompanyID: acme.ID, UserID: alice, Role: entity.RoleOwner,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: globex.ID, UserID: alice, Role: entity.RoleStaff,
	})

	_, err := h.resolver.Resolve(context.Background(), alice, uuid.Nil)
	if !errors.Is(err, middleware.ErrNoCompanyContext) {
		t.Fatalf("Resolve() = %v, want ErrNoCompanyContext for an ambiguous caller", err)
	}
}

// TestResolveHonoursExplicitChoice: with the header, the ambiguity is gone.
func TestResolveHonoursExplicitChoice(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	acme := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	globex := h.companies.seed(entity.Company{Code: "GLOBEX", Name: "Globex"})

	h.memberships.seed(entity.Membership{
		CompanyID: acme.ID, UserID: alice, Role: entity.RoleOwner,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: globex.ID, UserID: alice, Role: entity.RoleStaff,
	})

	got, err := h.resolver.Resolve(context.Background(), alice, globex.ID)
	if err != nil {
		t.Fatalf("Resolve() = %v", err)
	}

	if got.CompanyID != globex.ID {
		t.Errorf("company = %s, want %s", got.CompanyID, globex.ID)
	}
	// Role follows the membership, not the person.
	if got.Role != string(entity.RoleStaff) {
		t.Errorf("role = %q, want STAFF at Globex", got.Role)
	}
}

// TestResolveRejectsForeignCompany: naming a tenant you cannot reach is an
// error, never a silent fallback to your own.
func TestResolveRejectsForeignCompany(t *testing.T) {
	h := newHarness(t)
	alice, bob := uuid.New(), uuid.New()

	acme := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
	globex := h.companies.seed(entity.Company{Code: "GLOBEX", Name: "Globex"})

	h.memberships.seed(entity.Membership{
		CompanyID: acme.ID, UserID: alice, Role: entity.RoleOwner,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: globex.ID, UserID: bob, Role: entity.RoleOwner,
	})

	_, err := h.resolver.Resolve(context.Background(), alice, globex.ID)
	if err == nil {
		t.Fatal("Resolve() = nil for a company the caller cannot access")
	}
	if errors.Is(err, middleware.ErrNoCompanyContext) {
		t.Fatal("a foreign company was downgraded to no-context; it must be an error")
	}
	if code := apperror.From(err).Code; code != apperror.CodeForbidden {
		t.Errorf("code = %s, want FORBIDDEN", code)
	}
}

func TestResolveNoMemberships(t *testing.T) {
	h := newHarness(t)

	_, err := h.resolver.Resolve(context.Background(), uuid.New(), uuid.Nil)
	if !errors.Is(err, middleware.ErrNoCompanyContext) {
		t.Errorf("Resolve() = %v, want ErrNoCompanyContext", err)
	}
}

// TestResolveIgnoresPendingAndSuspendedMemberships: only ACTIVE grants a
// context, so an unaccepted invitation is not access.
func TestResolveIgnoresPendingAndSuspendedMemberships(t *testing.T) {
	for _, status := range []entity.MembershipStatus{
		entity.MembershipPending,
		entity.MembershipSuspended,
	} {
		t.Run(string(status), func(t *testing.T) {
			h := newHarness(t)
			alice := uuid.New()

			company := h.companies.seed(entity.Company{Code: "ACME", Name: "Acme"})
			h.memberships.seed(entity.Membership{
				CompanyID: company.ID, UserID: alice,
				Role: entity.RoleStaff, Status: status,
			})

			// Without a header: no context at all.
			if _, err := h.resolver.Resolve(context.Background(), alice, uuid.Nil); !errors.Is(
				err, middleware.ErrNoCompanyContext) {
				t.Errorf("default resolve = %v, want ErrNoCompanyContext", err)
			}

			// With a header: forbidden.
			if _, err := h.resolver.Resolve(context.Background(), alice, company.ID); err == nil {
				t.Errorf("explicit resolve succeeded for a %s membership", status)
			}
		})
	}
}

// TestResolveRejectsSuspendedCompany: suspending a tenant must take effect on
// the next request, not at the end of whatever session its members are in.
func TestResolveRejectsSuspendedCompany(t *testing.T) {
	h := newHarness(t)
	alice := uuid.New()

	company := h.companies.seed(entity.Company{
		Code: "SUSP", Name: "Suspended", Status: entity.CompanySuspended,
	})
	h.memberships.seed(entity.Membership{
		CompanyID: company.ID, UserID: alice, Role: entity.RoleOwner,
	})

	// Explicitly named: a clear error the caller can act on.
	if _, err := h.resolver.Resolve(context.Background(), alice, company.ID); err == nil {
		t.Error("Resolve() = nil for an explicitly named suspended company")
	}

	// Auto-selected: no-context rather than an error. The caller did not ask
	// for this company, so failing the whole request would strand them with no
	// way to switch elsewhere.
	_, err := h.resolver.Resolve(context.Background(), alice, uuid.Nil)
	if !errors.Is(err, middleware.ErrNoCompanyContext) {
		t.Errorf("default resolve = %v, want ErrNoCompanyContext", err)
	}
}
