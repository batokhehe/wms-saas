package bootstrap

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/middleware"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/internal/shared/validator"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// NewRouter builds the Gin engine and mounts every route.
//
// Note what this function does not contain: the name of any module, or the
// string "/api/v1". Both live in the registry, which is what lets a new module
// or a new API version arrive without editing this file.
func NewRouter(c *Container) *gin.Engine {
	if c.Config.App.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// Configure Gin's binding validator once, before any request is served.
	// It mutates global state, so doing it lazily per-request would be a race.
	validator.Init()

	// gin.New rather than gin.Default: Default installs Gin's own logger and
	// recovery, which would duplicate ours and write in a different format.
	engine := gin.New()

	engine.MaxMultipartMemory = 8 << 20 // 8 MiB

	// Trust no proxy by default. Deployments behind a load balancer must set
	// this explicitly, otherwise a client could spoof X-Forwarded-For and
	// defeat rate limiting or audit logging.
	_ = engine.SetTrustedProxies(nil)

	engine.RedirectTrailingSlash = true
	engine.HandleMethodNotAllowed = true

	// Middleware order is load-bearing:
	//
	//  1. RequestContext first — everything after it, including recovery, reads
	//     the request-scoped logger it creates.
	//  2. Recovery before the rest, so a panic anywhere downstream still yields
	//     a proper envelope.
	//  3. ErrorHandler wraps the handler chain to catch unwritten errors.
	//  4. AccessLog last of the observability set, so it measures the whole
	//     chain and sees the final status.
	engine.Use(
		middleware.RequestContext(c.Logger),
		middleware.Recovery(),
		middleware.ErrorHandler(),
		middleware.AccessLog(),
		middleware.SecurityHeaders(c.Config.App.IsProduction()),
		middleware.CORS(c.Config.HTTP.AllowedOrigins),
	)

	registerFallbacks(engine)

	// The registry mounts root routes and every supported API version.
	c.Registry.Mount(engine)

	return engine
}

// registerFallbacks makes unmatched routes return the standard envelope instead
// of Gin's default plain-text 404, so clients only ever parse one shape.
func registerFallbacks(engine *gin.Engine) {
	engine.NoRoute(func(c *gin.Context) {
		response.Error(c, apperror.NotFound("The requested endpoint does not exist").
			WithOp("http.router"))
	})

	engine.NoMethod(func(c *gin.Context) {
		response.Error(c, apperror.MethodNotAllowed("Method not allowed for this endpoint").
			WithOp("http.router"))
	})
}
