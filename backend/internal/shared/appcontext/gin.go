package appcontext

import (
	"context"

	"github.com/gin-gonic/gin"
)

// This file is the only bridge between Gin and the request context. Handlers
// call Context(c) once and pass the result downward; no layer below the handler
// ever sees a *gin.Context.

// SetGin stores rc on the Gin context and mirrors it into the underlying
// request's context.Context, so both retrieval styles resolve the same value.
func SetGin(c *gin.Context, rc *RequestContext) {
	c.Set(ginKey, rc)
	c.Request = c.Request.WithContext(Into(c.Request.Context(), rc))
}

// FromGin retrieves the RequestContext from a Gin context, falling back to the
// request's context.Context and finally to a safe default.
func FromGin(c *gin.Context) *RequestContext {
	if c == nil || c.Request == nil {
		return From(nil)
	}

	if value, exists := c.Get(ginKey); exists {
		if rc, ok := value.(*RequestContext); ok && rc != nil {
			return rc
		}
	}

	return From(c.Request.Context())
}

// Context returns the request's context.Context, which already carries the
// RequestContext. This is what a handler hands to its service:
//
//	result, err := h.service.Create(appcontext.Context(c), input)
func Context(c *gin.Context) context.Context {
	if c == nil || c.Request == nil {
		return context.Background()
	}
	return c.Request.Context()
}
