package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// HeaderCompanyID names the tenant a request is acting within.
//
// The active company travels in a header rather than in the access token
// because tokens carry no company claim (Authentication.md §2). That keeps
// switching companies free of credential reissue, and keeps a stolen token
// scoped to no tenant rather than permanently to one.
const HeaderCompanyID = "X-Company-ID"

// CompanyContext is the tenant resolved for a request.
type CompanyContext struct {
	CompanyID    uuid.UUID
	MembershipID uuid.UUID
	Role         string
}

// ErrNoCompanyContext is returned by a CompanyResolver when the caller holds no
// usable membership, or holds several and named none.
//
// It is a sentinel rather than an apperror because it is not always a failure:
// RequireCompany rejects it, while ResolveCompany treats it as "proceed
// anonymously with respect to tenancy".
var ErrNoCompanyContext = errors.New("middleware: no company context")

// CompanyResolver resolves the tenant a request is acting within.
//
// Declared HERE, in the consumer, rather than imported from the tenancy module
// — the same consumer-side interface pattern as TokenVerifier, and for the same
// reason. If middleware imported the tenancy module, every module needing a
// company context would transitively depend on tenancy internals, and the
// "modules never import each other" rule would break at the one dependency
// every future module has.
type CompanyResolver interface {
	// Resolve returns the company context for a user.
	//
	// requestedID is the company the client named, or uuid.Nil if it named
	// none. An implementation must return ErrNoCompanyContext when the caller
	// has no usable membership, and a FORBIDDEN apperror when they named a
	// company they cannot access — the two are different answers and the
	// middleware treats them differently.
	Resolve(ctx context.Context, userID, requestedID uuid.UUID) (CompanyContext, error)
}

// ResolveCompany attaches a company context when one can be determined, and
// proceeds without one otherwise.
//
// It is permissive by design and is paired with RequireCompany, which is the
// enforcing half. The split exists because some authenticated endpoints must
// work with NO company: creating your first company, listing the companies you
// belong to, and switching between them are all reachable before any tenant is
// active. A single enforcing middleware would make those endpoints unreachable
// for exactly the users who need them most.
//
// Must run after Authenticate: it reads the principal that middleware injected.
func ResolveCompany(resolver CompanyResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := appcontext.FromGin(c)

		// Unauthenticated requests have no tenant to resolve. Rejecting here
		// would duplicate Authenticate's job and would break the optional-auth
		// routes this middleware may sit behind.
		if !rc.IsAuthenticated() {
			c.Next()
			return
		}

		requested, err := requestedCompanyID(c)
		if err != nil {
			response.Error(c, err)
			return
		}

		resolved, err := resolver.Resolve(appcontext.Context(c), *rc.UserID, requested)
		if err != nil {
			if errors.Is(err, ErrNoCompanyContext) {
				// No usable membership, or several and none named. Proceed
				// without a tenant; RequireCompany rejects it if the endpoint
				// needs one.
				c.Next()
				return
			}
			// A named-but-inaccessible company is a real error and must not be
			// silently downgraded to "no context" — that would turn an attempted
			// cross-tenant access into a confusing success on the wrong tenant.
			response.Error(c, err)
			return
		}

		rc.WithCompany(resolved.CompanyID, resolved.MembershipID, resolved.Role)

		c.Next()
	}
}

// RequireCompany rejects a request that has no active company context.
//
// Applied to every tenant-scoped route. Services also call RequireTenant, so
// this is defence in depth rather than the only guard — but failing at the edge
// produces a clearer message than a service-layer error, and does it before any
// handler work happens.
func RequireCompany() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !appcontext.FromGin(c).HasTenant() {
			response.Error(c, apperror.Forbidden(
				"An active company is required. Select one with the "+
					HeaderCompanyID+" header.").
				WithOp("http.require_company"))
			return
		}

		c.Next()
	}
}

// requestedCompanyID reads and validates the tenant header.
//
// An unparseable value is rejected rather than ignored: silently falling back
// to the default company would let a typo route a write into the wrong tenant,
// which is the single worst failure mode in a multi-tenant system.
func requestedCompanyID(c *gin.Context) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.GetHeader(HeaderCompanyID))
	if raw == "" {
		return uuid.Nil, nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.BadRequest(HeaderCompanyID + " must be a valid UUID").
			WithOp("http.resolve_company").
			WithCause(err)
	}

	return parsed, nil
}
