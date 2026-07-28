// Package route declares the customer module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/customer/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	customers *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/customers")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			customers.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			customers.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			customers.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			customers.Update)

		// Activation and deactivation require customer.activate, NOT
		// customer.update. Editing a phone number is routine; deactivating a
		// customer stops every new sales order to them.
		scoped.PATCH("/:id/activate",
			middleware.RequirePermission(entity.PermissionActivate),
			customers.Activate)

		scoped.PATCH("/:id/deactivate",
			middleware.RequirePermission(entity.PermissionActivate),
			customers.Deactivate)
	}
}
