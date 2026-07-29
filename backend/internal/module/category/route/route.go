package route

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/category/handler"
	"github.com/gin-gonic/gin"
)

func RegisterV1(rg *gin.RouterGroup, h *handler.Handler, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver) {
	scoped := rg.Group("/categories")
	scoped.Use(middleware.Authenticate(verifier), middleware.ResolveCompany(companies), middleware.RequireCompany(), middleware.LoadPermissions(permissions))
	{
		scoped.GET("", middleware.RequirePermission("category.read"), h.List)
		scoped.POST("", middleware.RequirePermission("category.create"), h.Create)
		scoped.GET("/:id", middleware.RequirePermission("category.read"), h.Get)
		scoped.PUT("/:id", middleware.RequirePermission("category.update"), h.Update)
		scoped.PATCH("/:id/activate", middleware.RequirePermission("category.update"), h.Activate)
		scoped.PATCH("/:id/deactivate", middleware.RequirePermission("category.update"), h.Deactivate)
		scoped.PATCH("/:id/archive", middleware.RequirePermission("category.delete"), h.Archive)
		scoped.DELETE("/:id", middleware.RequirePermission("category.delete"), h.Archive)
	}
}
