// Package route declares the health module's URL layout.
//
// Keeping routes in their own package means the module's entire surface area
// can be reviewed in one short file, without reading handler bodies.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/module/health/handler"
)

// RegisterRoot mounts the probes at the unversioned root.
//
// Health is the one module that mounts outside /api/v{n}: orchestrators and
// load balancers are configured once and must not be asked to follow API
// versioning. Business modules never do this.
func RegisterRoot(rg *gin.RouterGroup, h *handler.Handler) {
	probes := rg.Group("/health")
	{
		probes.GET("", h.Ready)
		probes.GET("/live", h.Live)
		probes.GET("/ready", h.Ready)
	}
}

// RegisterV1 mounts the same probes under /api/v1 for clients that prefer a
// single versioned base URL.
func RegisterV1(rg *gin.RouterGroup, h *handler.Handler) {
	probes := rg.Group("/health")
	{
		probes.GET("", h.Ready)
		probes.GET("/live", h.Live)
		probes.GET("/ready", h.Ready)
	}
}
