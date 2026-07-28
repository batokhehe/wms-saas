// Package route declares the RBAC module's URL layout.
//
// This file is the authorisation policy. Every route states the permission it
// requires, so "who can do this?" is answered by reading one short file rather
// than by grepping handlers for scattered checks.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/rbac/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The middleware chain is ordered and every step depends on the one before:
//
//	Authenticate    → WHO the caller is
//	ResolveCompany  → WHERE they are acting
//	RequireCompany  → refuse if nowhere
//	LoadPermissions → WHAT they may do there (one query, cached per request)
//	RequirePermission → per route
//
// LoadPermissions must follow ResolveCompany because it reads the company and
// role that middleware injected; permissions are meaningless without a tenant.
func RegisterV1(
	rg *gin.RouterGroup,
	roles *handler.RoleHandler,
	permissions *handler.PermissionHandler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	resolver middleware.PermissionResolver,
) {
	scoped := rg.Group("")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(resolver),
	)

	// ---------- Roles ----------
	//
	// Create and Delete are restricted to OWNER by permission rather than by a
	// role-name check: role.create and role.delete are granted only to the
	// OWNER system role (see entity.DefaultPermissions), so the rule "only
	// OWNER may create or delete roles" is expressed as data an administrator
	// could in principle change, rather than as a hard-coded name comparison.
	//
	// That is the right shape for RBAC. A name check would make the role system
	// decorative — the point of permissions is that authority is granted, not
	// hard-wired. The OWNER role's own grants are immutable (see
	// RoleService.SetPermissions), so the guarantee still holds in practice.
	{
		scoped.GET("/roles",
			middleware.RequirePermission(entity.RoleRead.String()),
			roles.List)

		scoped.POST("/roles",
			middleware.RequirePermission(entity.RoleCreate.String()),
			roles.Create)

		scoped.PUT("/roles/:id",
			middleware.RequirePermission(entity.RoleUpdate.String()),
			roles.Update)

		scoped.DELETE("/roles/:id",
			middleware.RequirePermission(entity.RoleDelete.String()),
			roles.Delete)

		// Assigning permissions is a distinct capability from updating a role's
		// description: one changes what people can do, the other changes a
		// label. Granting them together would mean anyone who can rename a role
		// can also escalate it.
		scoped.PUT("/roles/:id/permissions",
			middleware.RequirePermission(entity.RoleAssignPermissions.String()),
			roles.SetPermissions)
	}

	// ---------- Permissions ----------
	{
		scoped.GET("/permissions",
			middleware.RequirePermission(entity.PermissionRead.String()),
			permissions.List)

		// Unguarded: a caller must always be able to discover what they
		// themselves may do. Requiring permission.read to find out whether you
		// hold permission.read is circular, and a client cannot render a usable
		// interface without it.
		scoped.GET("/permissions/mine", permissions.Mine)
	}
}
