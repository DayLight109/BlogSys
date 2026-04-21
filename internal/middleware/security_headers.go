package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets conservative defaults that matter for a JSON API:
//   - X-Content-Type-Options:nosniff     stop MIME-sniffing (e.g. of /uploads)
//   - X-Frame-Options:DENY               no iframing this host anywhere
//   - Referrer-Policy:strict-origin…     leak less Referer on outbound links
//   - Permissions-Policy                 deny motion sensors / mic / cam etc.
//
// HSTS is only meaningful over HTTPS; we add it when `enableHSTS` is true.
func SecurityHeaders(enableHSTS bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=()")
		if enableHSTS {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}
