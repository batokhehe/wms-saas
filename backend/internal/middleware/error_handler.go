package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
)

// ErrorHandler is the safety net for errors attached with c.Error() that were
// never converted into a response.
//
// The primary path is a handler calling response.Error and returning. This
// middleware catches the case where a handler pushed an error onto the Gin
// context and fell through without writing anything — which would otherwise
// send a bare 200 with an empty body and be very hard to diagnose.
//
// It runs after c.Next() and checks whether a response was actually written, so
// it never double-writes over a handler that behaved correctly.
func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Written reports whether any bytes or a status have gone out. If so,
		// the response is already committed and must not be touched.
		if c.Writer.Written() || len(c.Errors) == 0 {
			return
		}

		response.Error(c, c.Errors.Last().Err)
	}
}
