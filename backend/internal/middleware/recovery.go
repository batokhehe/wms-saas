package middleware

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/batokhehe/wms-saas/backend/internal/shared/appcontext"
	"github.com/batokhehe/wms-saas/backend/internal/shared/response"
	"github.com/batokhehe/wms-saas/backend/pkg/apperror"
)

// Recovery converts a panic in any handler into a standard error envelope
// instead of killing the process.
//
// Gin ships its own recovery middleware, but it writes plain text and logs in
// its own format. This version routes the failure through response.Error, so a
// panic produces exactly the same envelope as any other 500 — which is what
// makes the "one response format" rule hold even for bugs.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}

			rc := appcontext.FromGin(c)

			// A client that hangs up mid-write surfaces as a panic on a broken
			// pipe. The connection is already gone, so there is nothing to
			// write back and nothing worth alerting on.
			if isBrokenPipe(rec) {
				rc.Logger.Warn("client closed connection",
					zap.String("path", c.Request.URL.Path),
				)
				c.Abort()
				return
			}

			rc.Logger.Error("panic recovered",
				zap.String("method", c.Request.Method),
				zap.String("path", c.Request.URL.Path),
				zap.Any("panic", rec),
				zap.ByteString("stack", debug.Stack()),
			)

			// The panic value becomes the wrapped cause: it reaches the logs
			// and never the client.
			response.Error(c, apperror.Internal("An unexpected error occurred").
				WithCause(fmt.Errorf("panic: %v", rec)).
				WithOp("http.recovery"))
		}()

		c.Next()
	}
}

// isBrokenPipe reports whether the recovered value is a severed connection
// rather than a genuine bug.
func isBrokenPipe(rec any) bool {
	err, ok := rec.(error)
	if !ok {
		return false
	}

	var netErr *net.OpError
	if !errors.As(err, &netErr) {
		return false
	}

	var sysErr *os.SyscallError
	if !errors.As(netErr.Err, &sysErr) {
		return false
	}

	msg := strings.ToLower(sysErr.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		// Windows equivalents.
		strings.Contains(msg, "forcibly closed") ||
		strings.Contains(msg, "established connection was aborted")
}
