package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
)

// HeaderRequestID propagates a correlation id between the Flutter client, any
// reverse proxy, and this service.
const HeaderRequestID = "X-Request-ID"

// RequestContext builds the per-request context and injects it into both the
// Gin context and the request's context.Context.
//
// It must run first in the chain: every middleware and handler after it assumes
// a RequestContext exists, and the logger it builds is what carries the
// correlation id into every subsequent log line.
//
// This replaces the previous standalone RequestID middleware. Correlation is
// now one facet of request identity rather than a separate concern, so tenant
// and principal can be attached later by auth middleware without a second
// mechanism.
func RequestContext(base *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Honour an inbound id so a trace started at the edge stays intact, but
		// only if it is a well-formed UUID: an unvalidated header would let a
		// client inject arbitrary text into every log line for this request.
		id := c.GetHeader(HeaderRequestID)
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}

		rc := appcontext.New(id, base)
		rc.ClientIP = c.ClientIP()
		rc.UserAgent = c.Request.UserAgent()

		// CompanyID, UserID and Role stay nil here. Authentication middleware
		// will call rc.WithTenant() once it exists; nothing downstream changes.

		appcontext.SetGin(c, rc)
		c.Writer.Header().Set(HeaderRequestID, id)

		c.Next()
	}
}
