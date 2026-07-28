// Package route declares the tenancy module's URL layout.
//
// Keeping routes in one file means a reviewer can see at a glance which
// endpoints require an active company and which do not — the most
// security-relevant fact about a multi-tenant module — without reading a single
// handler body.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/tenancy/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// Every route here requires authentication. They divide into two groups:
//
//   - Tenant-OPTIONAL: reachable with no active company. Creating your first
//     company, listing the ones you belong to, and switching between them all
//     happen before any tenant is active, so requiring one would make them
//     unreachable for exactly the users who need them.
//   - Tenant-REQUIRED: everything that reads or writes within a company.
//
// The split is expressed as two Gin groups rather than per-handler checks, so
// adding a route to the wrong one is visible in the diff.
func RegisterV1(
	rg *gin.RouterGroup,
	companies *handler.CompanyHandler,
	memberships *handler.MembershipHandler,
	verifier middleware.TokenVerifier,
	resolver middleware.CompanyResolver,
) {
	// Authenticate establishes WHO; ResolveCompany establishes WHERE. The order
	// is load-bearing: the resolver reads the principal that Authenticate
	// injected, so reversing them would resolve a tenant for nobody.
	authed := rg.Group("")
	authed.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(resolver),
	)

	// ---------- Tenant-optional ----------
	{
		// Onboarding: the tail of the register flow. A user who has just
		// created an account has no company yet.
		authed.POST("/companies", companies.Create)

		// "Where can I work?" — the switcher menu.
		authed.GET("/companies", companies.List)
		authed.GET("/memberships/mine", memberships.Mine)

		// Choosing a tenant cannot itself require one.
		authed.POST("/companies/switch", companies.Switch)
	}

	// ---------- Tenant-required ----------
	scoped := authed.Group("")
	scoped.Use(middleware.RequireCompany())
	{
		scoped.GET("/companies/current", companies.Current)

		// Registered AFTER /companies/current and /companies/switch so those
		// literal segments are not captured by the :id parameter. Gin's router
		// prefers static segments over wildcards, but declaring them in this
		// order keeps the intent obvious to a reader.
		scoped.GET("/companies/:id", companies.Get)
		scoped.PUT("/companies/:id", companies.Update)
		scoped.DELETE("/companies/:id", companies.Delete)

		scoped.GET("/memberships", memberships.List)
		scoped.POST("/memberships/invite", memberships.Invite)
		scoped.DELETE("/memberships/:id", memberships.Remove)
	}
}
