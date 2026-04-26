package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
)

// decodeParam URL-decodes a path segment captured by Gin.
//
// In some Go/Gin version combinations, `c.Param(...)` returns the raw
// percent-encoded form (e.g. "%E5%85%B3…") even with UseRawPath and
// UnescapePathValues set. That makes DB lookups against raw-Chinese slugs
// miss. This helper is idempotent: strings with no '%' pass through unchanged,
// so it's safe to call unconditionally.
func decodeParam(s string) string {
	if d, err := url.PathUnescape(s); err == nil {
		return d
	}
	return s
}

func parseIDParam(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return id, true
}
