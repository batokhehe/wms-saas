package service

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/repository"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// ContextResolver resolves the tenant a request is acting within.
//
// It satisfies middleware.CompanyResolver. The interface is owned by the
// middleware package, so this type depends on middleware rather than the other
// way round — the same direction as the auth module's token verifier.
type ContextResolver struct {
	memberships repository.MembershipRepository
	companies   repository.CompanyRepository
}

var _ middleware.CompanyResolver = (*ContextResolver)(nil)

// NewContextResolver builds the resolver.
func NewContextResolver(
	memberships repository.MembershipRepository,
	companies repository.CompanyRepository,
) *ContextResolver {
	return &ContextResolver{memberships: memberships, companies: companies}
}

// Resolve determines the active company for a user.
//
// # Resolution order
//
//  1. A company named in the X-Company-ID header wins. It is validated against
//     the caller's ACTIVE memberships, so naming a tenant you cannot reach is a
//     403 — never a silent fallback.
//  2. Otherwise, if the caller holds exactly ONE active membership, use it.
//     This is the overwhelmingly common case (a person at one company) and
//     making them send a header for it would be pointless friction.
//  3. Otherwise — zero memberships, or several with none named — return
//     ErrNoCompanyContext.
//
// Rule 3 is the important one. Auto-selecting for a multi-company user would
// mean a request without a header lands in whichever tenant happened to sort
// first, and a warehouse operator working for two clients could ship stock from
// the wrong one without any error being raised. Ambiguity must be refused, not
// guessed.
func (r *ContextResolver) Resolve(
	ctx context.Context,
	userID, requestedID uuid.UUID,
) (middleware.CompanyContext, error) {
	if requestedID != uuid.Nil {
		return r.resolveRequested(ctx, userID, requestedID)
	}
	return r.resolveDefault(ctx, userID)
}

// resolveRequested validates an explicitly named company.
func (r *ContextResolver) resolveRequested(
	ctx context.Context,
	userID, companyID uuid.UUID,
) (middleware.CompanyContext, error) {
	membership, err := r.memberships.FindActiveByUserAndCompany(ctx, userID, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			// One message whether the company does not exist, the caller is not
			// a member, or their membership is PENDING or SUSPENDED.
			// Distinguishing them would let any authenticated user probe for the
			// existence of other tenants.
			return middleware.CompanyContext{}, apperror.Forbidden(
				"You do not have access to this company").
				WithOp("tenancy.resolver.Resolve")
		}
		return middleware.CompanyContext{}, err
	}

	if err := r.assertOperational(ctx, companyID); err != nil {
		return middleware.CompanyContext{}, err
	}

	return contextFrom(membership), nil
}

// resolveDefault picks the caller's only company, if they have exactly one.
func (r *ContextResolver) resolveDefault(
	ctx context.Context,
	userID uuid.UUID,
) (middleware.CompanyContext, error) {
	memberships, err := r.memberships.ListActiveByUser(ctx, userID)
	if err != nil {
		return middleware.CompanyContext{}, err
	}

	if len(memberships) != 1 {
		// Zero: the user belongs to nothing yet — they must create a company.
		// Several: ambiguous, and guessing is unsafe. Both are "no context",
		// which RequireCompany turns into an actionable 403 on routes that need
		// a tenant.
		return middleware.CompanyContext{}, middleware.ErrNoCompanyContext
	}

	membership := &memberships[0]

	// A suspended tenant must not silently become someone's working context.
	// Reported as no-context rather than an error, because the caller did not
	// ask for this company — the system chose it on their behalf, so failing
	// the whole request would strand them with no way to switch.
	if err := r.assertOperational(ctx, membership.CompanyID); err != nil {
		return middleware.CompanyContext{}, middleware.ErrNoCompanyContext
	}

	return contextFrom(membership), nil
}

// assertOperational rejects a company that is not ACTIVE.
//
// Checked on every request rather than only at switch time: suspending a tenant
// for non-payment must take effect immediately, not at the end of whatever
// session its members happen to be in.
func (r *ContextResolver) assertOperational(ctx context.Context, companyID uuid.UUID) error {
	// Unscoped is correct: the ACTIVE membership the caller has already been
	// shown to hold IS the access check. Re-running the reachability subquery
	// would repeat that work on every single request.
	company, err := r.companies.FindByIDUnscoped(ctx, companyID)
	if err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			// The company was soft-deleted while the membership survived.
			return apperror.Forbidden("You do not have access to this company").
				WithOp("tenancy.resolver.assertOperational")
		}
		return err
	}

	if !company.IsOperational() {
		return apperror.Forbidden("This company is not active").
			WithOp("tenancy.resolver.assertOperational")
	}

	return nil
}

func contextFrom(m *entity.Membership) middleware.CompanyContext {
	return middleware.CompanyContext{
		CompanyID:    m.CompanyID,
		MembershipID: m.ID,
		Role:         string(m.Role),
	}
}
