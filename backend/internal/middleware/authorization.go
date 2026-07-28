package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// PermissionResolver returns the permission codes a role grants in a company.
//
// Declared HERE, in the consumer, rather than imported from the RBAC module —
// the same consumer-side interface pattern as TokenVerifier and CompanyResolver,
// and for the same reason. If middleware imported RBAC, every module needing an
// authorisation check would transitively depend on RBAC internals, and the
// "modules never import each other" rule would break at the one dependency
// every future module has.
//
// Codes are plain strings rather than a typed Code so that this package holds
// no RBAC vocabulary at all.
type PermissionResolver interface {
	// Resolve returns the codes granted to roleName within companyID.
	//
	// It must fail CLOSED: an unknown role or a resolution miss returns an
	// empty slice, not an error and not a permissive default.
	Resolve(ctx context.Context, companyID uuid.UUID, roleName string) ([]string, error)
}

// permissionsKey is the *gin.Context key holding the resolved permission set.
// Unexported so nothing outside this package can inject permissions.
const permissionsKey = "app.permissions"

// LoadPermissions resolves the caller's permissions once per request.
//
// Running it once and caching on the context matters: without it, a route
// guarded by three permissions would hit the database three times, and a
// handler asking "may this user also do X?" would add a fourth.
//
// It is permissive — a caller with no company or no permissions proceeds with an
// empty set — because RequirePermission is the enforcing half. The split
// mirrors ResolveCompany/RequireCompany and exists for the same reason: some
// authenticated routes legitimately need no permission at all.
//
// Must run after ResolveCompany, whose company and role it reads.
func LoadPermissions(resolver PermissionResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := appcontext.FromGin(c)

		// No tenant means no permissions: authorisation is evaluated WITHIN a
		// company, so there is nothing to resolve against.
		if !rc.HasTenant() || rc.Role == "" {
			setPermissions(c, map[string]struct{}{})
			c.Next()
			return
		}

		codes, err := resolver.Resolve(appcontext.Context(c), *rc.CompanyID, rc.Role)
		if err != nil {
			// A resolution FAILURE is different from a resolution MISS. A miss
			// yields an empty set and a clean 403; a failure means the database
			// is unreachable, and continuing with an empty set would present an
			// infrastructure outage as an authorisation decision.
			response.Error(c, err)
			return
		}

		set := make(map[string]struct{}, len(codes))
		for _, code := range codes {
			set[code] = struct{}{}
		}
		setPermissions(c, set)

		c.Next()
	}
}

// RequirePermission rejects a request whose caller lacks every listed code.
//
// Semantics are ALL, not ANY: a route declaring two permissions requires both.
// That is the conservative reading — a reviewer scanning a route table reads
// "needs role.create and role.assign_permissions" as a conjunction, and making
// it a disjunction would quietly widen access.
//
// Declaring no codes is a programming error and denies the request. An empty
// requirement almost always means a constant was mistyped or a slice was built
// dynamically and came out empty, and failing open there would silently
// unguard a route.
func RequirePermission(codes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(codes) == 0 {
			response.Error(c, apperror.Internal("No permission was declared for this route").
				WithOp("http.require_permission"))
			return
		}

		granted := permissionsFrom(c)

		for _, code := range codes {
			if _, ok := granted[code]; !ok {
				// The response names the missing permission. That is a
				// deliberate trade: it tells a legitimate user precisely what
				// to ask their administrator for, and it reveals nothing an
				// attacker could not read from the public API documentation —
				// the codes are a fixed, published catalogue.
				response.Error(c, apperror.Forbidden(
					"You do not have permission to perform this action").
					WithDetails(map[string]any{"required_permission": code}).
					WithOp("http.require_permission"))
				return
			}
		}

		c.Next()
	}
}

// RequireAnyPermission rejects a request whose caller holds none of the codes.
//
// For actions that several distinct permissions each independently justify.
// Kept separate from RequirePermission rather than adding a mode flag, so the
// semantics are visible at the route declaration.
func RequireAnyPermission(codes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if len(codes) == 0 {
			response.Error(c, apperror.Internal("No permission was declared for this route").
				WithOp("http.require_any_permission"))
			return
		}

		granted := permissionsFrom(c)

		for _, code := range codes {
			if _, ok := granted[code]; ok {
				c.Next()
				return
			}
		}

		response.Error(c, apperror.Forbidden(
			"You do not have permission to perform this action").
			WithDetails(map[string]any{"required_any_permission": codes}).
			WithOp("http.require_any_permission"))
	}
}

// Permissions returns the codes resolved for the current request.
//
// Exposed so a handler can render "what may I do?" for a client. It must never
// be used to make an authorisation DECISION inside a handler — that is
// RequirePermission's job, and a check written in a handler is a check the
// route table does not show.
func Permissions(c *gin.Context) []string {
	granted := permissionsFrom(c)

	codes := make([]string, 0, len(granted))
	for code := range granted {
		codes = append(codes, code)
	}
	return codes
}

// HasPermission reports whether the current request holds a code.
//
// Same caveat as Permissions: for shaping a response, never for guarding one.
func HasPermission(c *gin.Context, code string) bool {
	_, ok := permissionsFrom(c)[code]
	return ok
}

func setPermissions(c *gin.Context, set map[string]struct{}) {
	c.Set(permissionsKey, set)
}

// permissionsFrom reads the resolved set, defaulting to empty.
//
// An absent value means LoadPermissions did not run, which is a wiring mistake.
// It returns an empty set so the failure mode is "denied everything" rather
// than a panic or, worse, a permissive default.
func permissionsFrom(c *gin.Context) map[string]struct{} {
	value, exists := c.Get(permissionsKey)
	if !exists {
		return map[string]struct{}{}
	}

	set, ok := value.(map[string]struct{})
	if !ok {
		return map[string]struct{}{}
	}

	return set
}
