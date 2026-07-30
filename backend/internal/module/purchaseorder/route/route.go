// Package route declares the purchase-order module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires, so "who can commit the company's money?" is answered by
// reading one short file rather than by grepping handlers.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/purchaseorder/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	orders *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/purchase-orders")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			orders.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			orders.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			orders.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			orders.Update)

		// Approval and cancellation are NOT purchaseorder.update. Editing a draft
		// is clerical; approving commits the company to buy something and unlocks
		// the inbound chain, and cancelling withdraws that commitment.
		scoped.POST("/:id/approve",
			middleware.RequirePermission(entity.PermissionApprove),
			orders.Approve)

		scoped.POST("/:id/cancel",
			middleware.RequirePermission(entity.PermissionCancel),
			orders.Cancel)
	}
}
