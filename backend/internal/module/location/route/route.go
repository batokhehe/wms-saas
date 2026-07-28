// Package route declares the location module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can do this?" is answered by reading one
// short file rather than by grepping handlers for scattered checks.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/location/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The middleware chain is the platform's full stack, and every step depends on
// the one before:
//
//	Authenticate     → WHO the caller is
//	ResolveCompany   → WHERE they are acting
//	RequireCompany   → refuse if nowhere
//	LoadPermissions  → WHAT they may do there (one query, cached per request)
//	RequirePermission → per route
func RegisterV1(
	rg *gin.RouterGroup,
	locations *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/locations")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			locations.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			locations.Create)

		// Registered BEFORE /:id so the literal "barcode" segment is not
		// captured by the wildcard. Gin prefers static segments, but declaring
		// them in this order keeps the intent obvious to a reader.
		scoped.GET("/barcode/:barcode",
			middleware.RequirePermission(entity.PermissionRead),
			locations.GetByBarcode)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			locations.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.Update)

		// Capacity and barcode are ordinary updates: relabelling a bin or
		// re-declaring its weight limit is routine maintenance of the layout.
		scoped.PATCH("/:id/capacity",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.ChangeCapacity)

		scoped.PATCH("/:id/barcode",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.AssignBarcode)

		// ---------- Availability ----------
		//
		// Activate, deactivate and maintenance are update-level: they adjust
		// whether a place is in the layout, which is the same class of decision
		// as changing its capacity.
		scoped.PATCH("/:id/activate",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.Activate)

		scoped.PATCH("/:id/deactivate",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.Deactivate)

		scoped.PATCH("/:id/maintenance",
			middleware.RequirePermission(entity.PermissionUpdate),
			locations.StartMaintenance)

		// ---------- Holds and retirement ----------
		//
		// Locking requires its own permission. A lock blocks every putaway and
		// pick that would have used the location, and it carries a reason
		// someone else must later judge safe to clear — a heavier decision than
		// adjusting a shelf's weight limit.
		scoped.PATCH("/:id/lock",
			middleware.RequirePermission(entity.PermissionLock),
			locations.Lock)

		scoped.PATCH("/:id/unlock",
			middleware.RequirePermission(entity.PermissionLock),
			locations.Unlock)

		// Archiving removes a place from the layout permanently, so it is
		// grouped with locking rather than with update.
		scoped.DELETE("/:id",
			middleware.RequirePermission(entity.PermissionLock),
			locations.Archive)
	}
}
