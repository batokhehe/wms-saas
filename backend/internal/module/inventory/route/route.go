// Package route declares the inventory module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can do this?" is answered by reading one short
// file rather than by grepping handlers for scattered checks.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/inventory/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The middleware chain is the platform's full stack:
//
//	Authenticate      → WHO the caller is
//	ResolveCompany    → WHERE they are acting
//	RequireCompany    → refuse if nowhere
//	LoadPermissions   → WHAT they may do there
//	RequirePermission → per route
func RegisterV1(
	rg *gin.RouterGroup,
	inventories *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/inventories")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			inventories.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			inventories.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			inventories.Get)

		// ---------- Quantity movements ----------
		//
		// Increase and decrease are routine stock movement, so they share
		// inventory.update. Reserving, transferring, adjusting, locking and
		// counting each require their own permission because each is a distinct
		// operational decision with a different blast radius.
		scoped.POST("/:id/increase",
			middleware.RequirePermission(entity.PermissionUpdate),
			inventories.Increase)

		scoped.POST("/:id/decrease",
			middleware.RequirePermission(entity.PermissionUpdate),
			inventories.Decrease)

		// ---------- Reservations ----------
		scoped.POST("/:id/reserve",
			middleware.RequirePermission(entity.PermissionReserve),
			inventories.Reserve)

		scoped.POST("/:id/release",
			middleware.RequirePermission(entity.PermissionReserve),
			inventories.Release)

		// ---------- Transfers ----------
		scoped.POST("/:id/transfer-out",
			middleware.RequirePermission(entity.PermissionTransfer),
			inventories.TransferOut)

		scoped.POST("/:id/transfer-in",
			middleware.RequirePermission(entity.PermissionTransfer),
			inventories.TransferIn)

		// ---------- Corrections ----------
		//
		// Adjustment overrides the recorded count with no physical count behind
		// it, so it requires its own governance-sensitive permission — distinct
		// from a cycle count, which reconciles to a real count.
		scoped.POST("/:id/adjust",
			middleware.RequirePermission(entity.PermissionAdjust),
			inventories.Adjust)

		scoped.POST("/:id/cycle-count",
			middleware.RequirePermission(entity.PermissionCycleCount),
			inventories.CycleCount)

		// ---------- Governance hold ----------
		scoped.POST("/:id/lock",
			middleware.RequirePermission(entity.PermissionLock),
			inventories.Lock)

		scoped.POST("/:id/unlock",
			middleware.RequirePermission(entity.PermissionLock),
			inventories.Unlock)
	}
}
