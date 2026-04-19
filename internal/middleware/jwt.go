package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/lilce/blog-api/internal/auth"
)

const (
	CtxUserID   = "ctx_user_id"
	CtxUsername = "ctx_username"
	CtxRole     = "ctx_role"
)

func JWTAuth(tm *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := tm.Parse(tokenStr)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		if claims.Type != auth.TypeAccess {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "expected access token"})
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}

func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxRole)
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}
		c.Next()
	}
}
