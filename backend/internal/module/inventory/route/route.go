// Package route declares the inventory module's URL layout.
//
// This file is the module's authorisation policy: every route states the
// permission it requires, so "who can move stock?" is answered by reading one
// short file rather than by grepping handlers.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	positions *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/inventory-positions")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			positions.List)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			positions.Get)

		// Receiving opens a position when none exists, so it is addressed by stock
		// KEY and carries the create permission.
		scoped.POST("/receive",
			middleware.RequirePermission(entity.PermissionCreate),
			positions.Receive)

		// ---------- Quantity movement ----------
		scoped.POST("/:id/issue",
			middleware.RequirePermission(entity.PermissionUpdate),
			positions.Issue)

		// ---------- Reservation and allocation ----------
		//
		// Allocation hardens a reservation, so both stages share the reserve
		// permission: whoever may promise stock may also assign the promise.
		scoped.POST("/:id/reserve",
			middleware.RequirePermission(entity.PermissionReserve),
			positions.Reserve)

		scoped.POST("/:id/release",
			middleware.RequirePermission(entity.PermissionReserve),
			positions.Release)

		scoped.POST("/:id/allocate",
			middleware.RequirePermission(entity.PermissionReserve),
			positions.Allocate)

		scoped.POST("/:id/deallocate",
			middleware.RequirePermission(entity.PermissionReserve),
			positions.Deallocate)

		// ---------- Quarantine ----------
		scoped.POST("/:id/quarantine",
			middleware.RequirePermission(entity.PermissionLock),
			positions.Quarantine)

		scoped.POST("/:id/release-quarantine",
			middleware.RequirePermission(entity.PermissionLock),
			positions.ReleaseQuarantine)

		// ---------- Transfer ----------
		scoped.POST("/:id/transfer",
			middleware.RequirePermission(entity.PermissionTransfer),
			positions.Transfer)

		// ---------- Correction ----------
		//
		// An adjustment overrides the recorded count, so it keeps its own
		// governance-sensitive permission — distinct from routine movement.
		scoped.POST("/:id/adjust",
			middleware.RequirePermission(entity.PermissionAdjust),
			positions.Adjust)
	}
}
