package middleware

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// skipLogPaths are polled constantly by orchestrators and load balancers.
// Logging them would bury real traffic under probe noise.
var skipLogPaths = map[string]struct{}{
	"/health":       {},
	"/health/live":  {},
	"/health/ready": {},
	"/metrics":      {},
}

// AccessLog emits one structured line per completed request.
//
// It no longer builds the request logger — RequestContext does that, so this
// middleware only reports outcomes. Splitting the two means the logger exists
// before any other middleware runs, including recovery.
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		if _, skip := skipLogPaths[path]; skip {
			return
		}

		rc := appcontext.FromGin(c)
		status := c.Writer.Status()

		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Duration("latency", rc.Elapsed()),
			zap.String("client_ip", rc.ClientIP),
			zap.Int("bytes", c.Writer.Size()),
		}

		if query != "" {
			fields = append(fields, zap.String("query", query))
		}
		if tenant := rc.CompanyIDOrNil(); tenant != "" {
			fields = append(fields, zap.String("company_id", tenant))
		}

		// The response layer already logged the error with its full detail; here
		// only the code is echoed so the access log stays one line per request.
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("error", c.Errors.Last().Error()))
		}

		// Level tracks the outcome so alerting can key off level alone.
		switch {
		case status >= 500:
			rc.Logger.Error("request failed", fields...)
		case status >= 400:
			rc.Logger.Warn("request rejected", fields...)
		default:
			rc.Logger.Info("request completed", fields...)
		}
	}
}
