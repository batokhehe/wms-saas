// Package route declares the supplier module's URL layout.
//
// This file is the module's authorisation policy. Every route states the
// permission it requires.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/entity"
	"github.com/batokhehe/wms-saas/backend/internal/module/supplier/handler"
)

// RegisterV1 mounts the module under /api/v1.
func RegisterV1(
	rg *gin.RouterGroup,
	suppliers *handler.Handler,
	verifier middleware.TokenVerifier,
	companies middleware.CompanyResolver,
	permissions middleware.PermissionResolver,
) {
	scoped := rg.Group("/suppliers")
	scoped.Use(
		middleware.Authenticate(verifier),
		middleware.ResolveCompany(companies),
		middleware.RequireCompany(),
		middleware.LoadPermissions(permissions),
	)

	{
		scoped.GET("",
			middleware.RequirePermission(entity.PermissionRead),
			suppliers.List)

		scoped.POST("",
			middleware.RequirePermission(entity.PermissionCreate),
			suppliers.Create)

		scoped.GET("/:id",
			middleware.RequirePermission(entity.PermissionRead),
			suppliers.Get)

		scoped.PUT("/:id",
			middleware.RequirePermission(entity.PermissionUpdate),
			suppliers.Update)

		// Activation and deactivation require supplier.activate, NOT
		// supplier.update. Editing a phone number is routine; deactivating a
		// supplier stops every new purchase order to them.
		scoped.PATCH("/:id/activate",
			middleware.RequirePermission(entity.PermissionActivate),
			suppliers.Activate)

		scoped.PATCH("/:id/deactivate",
			middleware.RequirePermission(entity.PermissionActivate),
			suppliers.Deactivate)
	}
}
