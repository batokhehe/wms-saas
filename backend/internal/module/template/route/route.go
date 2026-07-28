// Package route declares the module's URL layout.
//
// Keeping routes in one small file means a reviewer can audit the module's
// entire public surface — and its middleware, which is where authorisation will
// be applied — without reading a single handler body.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/template/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The group receives only the module's own path segment: the "/api/v1" prefix
// is supplied by the registry. A module that hard-coded its version prefix here
// would break the moment v2 arrived.
func RegisterV1(rg *gin.RouterGroup, h *handler.Handler) {
	// TEMPLATE: rename "resources" to the real collection name, in plural.
	resources := rg.Group("/resources")

	// Route-level middleware goes here, e.g. once auth exists:
	//   resources.Use(middleware.Authenticate(), middleware.RequireRole("admin"))
	{
		resources.POST("", h.Create)
		resources.GET("", h.List)
		resources.GET("/:id", h.Get)
		resources.PATCH("/:id", h.Update)
		resources.DELETE("/:id", h.Delete)
	}
}
