package route

import (
	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/product/handler"
	"github.com/gin-gonic/gin"
)

func RegisterV1(rg *gin.RouterGroup, h *handler.Handler, verifier middleware.TokenVerifier, companies middleware.CompanyResolver, permissions middleware.PermissionResolver) {
	scoped := rg.Group("/products")
	scoped.Use(middleware.Authenticate(verifier), middleware.ResolveCompany(companies), middleware.RequireCompany(), middleware.LoadPermissions(permissions))
	{
		scoped.GET("", middleware.RequirePermission("product.read"), h.List)
		scoped.POST("", middleware.RequirePermission("product.create"), h.Create)
		scoped.GET("/:id", middleware.RequirePermission("product.read"), h.Get)
		scoped.PUT("/:id", middleware.RequirePermission("product.update"), h.Update)
		scoped.PATCH("/:id/activate", middleware.RequirePermission("product.update"), h.Activate)
		scoped.PATCH("/:id/deactivate", middleware.RequirePermission("product.update"), h.Deactivate)
		scoped.PATCH("/:id/archive", middleware.RequirePermission("product.delete"), h.Archive)
	}
}
