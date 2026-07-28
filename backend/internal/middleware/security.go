package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets the baseline hardening headers on every response.
//
// The API serves JSON only, so the content-security policy can be maximally
// restrictive: nothing is ever meant to be loaded or framed from a response.
func SecurityHeaders(isProduction bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()

		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")

		// HSTS is only meaningful over TLS, and setting it in development would
		// pin localhost to HTTPS in the developer's browser.
		if isProduction {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}
