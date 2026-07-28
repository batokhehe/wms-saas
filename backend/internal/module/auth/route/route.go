// Package route declares the auth module's URL layout.
//
// Keeping routes in one file means a reviewer can see at a glance which
// endpoints are public and which require a token — the most security-relevant
// fact about the module — without reading a single handler body.
package route

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/module/auth/handler"
)

// RegisterV1 mounts the module under /api/v1.
//
// The "/api/v1" prefix is supplied by the registry; a module that hard-coded
// its version would break the moment v2 arrived.
func RegisterV1(rg *gin.RouterGroup, h *handler.Handler, verifier middleware.TokenVerifier) {
	auth := rg.Group("/auth")

	// Public. These are the only unauthenticated write endpoints in the system,
	// which makes them the natural target for credential stuffing — they are
	// the first place a rate limiter belongs. See Security.md.
	{
		auth.POST("/register", h.Register)
		auth.POST("/login", h.Login)

		// Refresh and logout are public because a client reaching them has, by
		// definition, an expired or unwanted access token. Their credential is
		// the refresh token in the body, which the service validates.
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", h.Logout)
	}

	// Protected. The middleware is applied to a sub-group rather than the whole
	// /auth group, so adding a route to the wrong block is visible in the diff.
	protected := auth.Group("")
	protected.Use(middleware.Authenticate(verifier))
	{
		protected.GET("/me", h.Me)
	}
}
