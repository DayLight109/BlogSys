package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit caps request body size via http.MaxBytesReader so a malicious
// client can't OOM the server with a huge JSON payload. Upload endpoints that
// need a larger limit can wrap the body themselves before this middleware,
// or use a route-level override.
func BodyLimit(max int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, max)
		}
		c.Next()
	}
}
