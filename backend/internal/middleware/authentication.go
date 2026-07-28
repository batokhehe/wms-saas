package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Principal is the identity carried by a verified access token.
type Principal struct {
	UserID uuid.UUID
	// Role is empty in this sprint. RBAC arrives with membership in Sprint 2,
	// and the field exists so that adding it changes the token service and this
	// struct rather than every consumer of the request context.
	Role string
}

// TokenVerifier validates an access token.
//
// It is declared HERE, in the consumer, rather than imported from the auth
// module — the consumer-side interface pattern required by ModuleConvention §6.
//
// This is what keeps the dependency arrow pointing the right way. If middleware
// imported the auth module, then every module using authentication would
// transitively depend on auth's internals, and the "modules never import each
// other" rule would be broken by the one piece of code every module needs.
// Instead bootstrap injects auth's token service, which happens to satisfy this
// interface.
type TokenVerifier interface {
	// VerifyAccessToken returns the principal for a raw bearer token, or an
	// error. Implementations must return a generic error: distinguishing
	// "expired" from "bad signature" tells an attacker which part of a forged
	// token to fix.
	VerifyAccessToken(raw string) (Principal, error)
}

const (
	headerAuthorization = "Authorization"
	bearerPrefix        = "Bearer "
)

// Authenticate rejects requests without a valid access token and injects the
// principal into the RequestContext.
//
// CompanyID is deliberately left nil. Identity is independent of company in
// this sprint: a user belongs to no company yet, and the tenant will be
// resolved by a separate middleware once membership exists. Every layer below
// already reads CompanyID from the context, so that addition changes no
// service and no repository.
func Authenticate(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c)
		if err != nil {
			response.Error(c, err)
			return
		}

		principal, err := verifier.VerifyAccessToken(raw)
		if err != nil {
			response.Error(c, err)
			return
		}

		// Attaches the principal and re-tags the request logger, so every log
		// line after this point is attributable to the authenticated user.
		rc := appcontext.FromGin(c)
		rc.WithTenant(nil, &principal.UserID, principal.Role)

		c.Next()
	}
}

// OptionalAuthenticate attaches a principal when a valid token is present and
// proceeds anonymously otherwise.
//
// For endpoints whose response varies by authentication but which do not
// require it. A malformed token is ignored rather than rejected, because on
// these routes the caller never asked to be authenticated.
func OptionalAuthenticate(verifier TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearerToken(c)
		if err != nil {
			c.Next()
			return
		}

		principal, err := verifier.VerifyAccessToken(raw)
		if err != nil {
			c.Next()
			return
		}

		appcontext.FromGin(c).WithTenant(nil, &principal.UserID, principal.Role)
		c.Next()
	}
}

// bearerToken extracts the credential from the Authorization header.
//
// Only the Authorization header is accepted — never a query parameter. URLs are
// written to access logs, proxy logs, browser history and Referer headers, so a
// token in a query string is a token in half a dozen places nobody is guarding.
func bearerToken(c *gin.Context) (string, error) {
	missing := apperror.Unauthorized("An access token is required").
		WithOp("http.authenticate")

	header := c.GetHeader(headerAuthorization)
	if header == "" {
		return "", missing
	}

	// Case-insensitive scheme match: RFC 7235 defines the scheme as
	// case-insensitive, and clients send "bearer" as well as "Bearer".
	if len(header) <= len(bearerPrefix) ||
		!strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", missing
	}

	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return "", missing
	}

	return token, nil
}
