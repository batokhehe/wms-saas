package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS applies an allow-list based cross-origin policy.
//
// The allow-list is echoed back one origin at a time rather than as "*",
// because credentialed requests (the Flutter web build sends an Authorization
// header) are rejected by browsers when the wildcard is used.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 1 && allowedOrigins[0] == "*"

	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		allowed[strings.TrimSpace(origin)] = struct{}{}
	}

	allowedHeaders := strings.Join([]string{
		"Origin", "Content-Type", "Accept", "Authorization",
		HeaderRequestID, "X-Tenant-ID",
	}, ", ")

	allowedMethods := strings.Join([]string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}, ", ")

	maxAge := strconv.Itoa(int((12 * time.Hour).Seconds()))

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if origin != "" {
			_, ok := allowed[origin]
			if allowAll || ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Methods", allowedMethods)
				c.Header("Access-Control-Allow-Headers", allowedHeaders)
				c.Header("Access-Control-Expose-Headers", HeaderRequestID)
				c.Header("Access-Control-Max-Age", maxAge)
				// Caches must key on Origin, otherwise one origin's allowed
				// response could be served to another.
				c.Header("Vary", "Origin")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
