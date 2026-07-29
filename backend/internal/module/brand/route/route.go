package route

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/brand/handler"
	"github.com/gin-gonic/gin"
)

func RegisterV1(rg *gin.RouterGroup, h *handler.Handler, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver) {
	scoped := rg.Group("/brands")
	scoped.Use(middleware.Authenticate(verifier), middleware.ResolveCompany(companies), middleware.RequireCompany(), middleware.LoadPermissions(permissions))
	{
		scoped.GET("", middleware.RequirePermission("brand.read"), h.List)
		scoped.POST("", middleware.RequirePermission("brand.create"), h.Create)
		scoped.GET("/:id", middleware.RequirePermission("brand.read"), h.Get)
		scoped.PUT("/:id", middleware.RequirePermission("brand.update"), h.Update)
		scoped.PATCH("/:id/activate", middleware.RequirePermission("brand.update"), h.Activate)
		scoped.PATCH("/:id/deactivate", middleware.RequirePermission("brand.update"), h.Deactivate)
		scoped.PATCH("/:id/archive", middleware.RequirePermission("brand.delete"), h.Archive)
		scoped.DELETE("/:id", middleware.RequirePermission("brand.delete"), h.Archive)
	}
}
