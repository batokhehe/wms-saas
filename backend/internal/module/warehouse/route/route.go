// Package route declares the warehouse module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can do this?" is answered by reading one
// short file rather than by grepping handlers for scattered checks.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/warehouse/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The middleware chain is the full stack the platform now provides, and every
// step depends on the one before:
//
//	Authenticate     → WHO the caller is
//	ResolveCompany   → WHERE they are acting
//	RequireCompany   → refuse if nowhere
//	LoadPermissions  → WHAT they may do there (one query, cached per request)
//	RequirePermission → per route
//
// This is the first business module to use all five, and it needed no change to
// any of them — which is the return on the previous four sprints.
func RegisterV1(
	rg *gin.RouterGroup,
	warehouses *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/warehouses")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			warehouses.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			warehouses.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			warehouses.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			warehouses.Update)

		// ---------- Lifecycle ----------
		//
		// Activation and deactivation require warehouse.activate, NOT
		// warehouse.update. Editing an address is routine; declaring a site fit
		// to receive and ship stock is a commissioning decision. Granting them
		// together would mean anyone who can fix a typo can also put a
		// half-configured warehouse into operation.
		scoped.PATCH("/:id/activate",
			middleware.RequirePermission(entity.PermissionActivate),
			warehouses.Activate)

		scoped.PATCH("/:id/deactivate",
			middleware.RequirePermission(entity.PermissionActivate),
			warehouses.Deactivate)

		// Suspension is a governance hold, not an operational toggle, so it
		// requires the destructive permission rather than the activation one.
		// An operator who can commission a site should not be able to place it
		// under a compliance block.
		scoped.PATCH("/:id/suspend",
			middleware.RequirePermission(entity.PermissionDelete),
			warehouses.Suspend)

		scoped.PATCH("/:id/contact",
			middleware.RequirePermission(entity.PermissionUpdate),
			warehouses.ChangeContact)

		// ---------- Retirement ----------
		//
		// Both routes call the same Archive operation — a warehouse is never
		// hard-deleted. DELETE is the REST-conventional alias clients expect;
		// PATCH /archive is the domain-language version. They share a handler
		// path so they cannot diverge, and share a permission so neither is a
		// way around the other.
		scoped.PATCH("/:id/archive",
			middleware.RequirePermission(entity.PermissionDelete),
			warehouses.Archive)

		scoped.DELETE("/:id",
			middleware.RequirePermission(entity.PermissionDelete),
			warehouses.Delete)
	}
}
