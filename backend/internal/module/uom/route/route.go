package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/uom/handler"
)

func RegisterV1(
	rg *gin.RouterGroup,
	uoms *handler.Handler,
	verifier middleware.TokenVerifier,
) {
	scoped := rg.Group("/uoms")
	scoped.Use(middleware.Authenticate(verifier))

	{
		scoped.GET("", uoms.List)
		scoped.POST("", uoms.Create)
		scoped.GET("/:id", uoms.Get)
		scoped.PUT("/:id", uoms.Update)
		scoped.PATCH("/:id/activate", uoms.Activate)
		scoped.PATCH("/:id/deactivate", uoms.Deactivate)
	}
}
