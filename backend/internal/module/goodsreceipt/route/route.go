// Package route declares the goods-receipt module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can create stock out of a delivery?" is
// answered by reading one short file rather than by grepping handlers.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/goodsreceipt/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	receipts *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/goods-receipts")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			receipts.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			receipts.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			receipts.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			receipts.Update)

		scoped.DELETE("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			receipts.Delete)

		scoped.POST("/:id/confirm",
			middleware.RequirePermission(entity.PermissionConfirm),
			receipts.Confirm)

		// RECEIVE is the step that creates stock and appends to the ledger. It
		// carries its own permission because it is the only one here that changes
		// a balance.
		scoped.POST("/:id/receive",
			middleware.RequirePermission(entity.PermissionReceive),
			receipts.Receive)

		scoped.POST("/:id/cancel",
			middleware.RequirePermission(entity.PermissionCancel),
			receipts.Cancel)
	}
}
