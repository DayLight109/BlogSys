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

// JWTAuth I don't know, I don't wanna know, I just write it down
// please don't let me write JWT bundle again xD
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

// SoftJWTAuth tries to parse an Authorization Bearer token and attaches
// claims to the gin context when valid. Unlike JWTAuth, missing or invalid
// tokens DO NOT abort the request — the handler proceeds anonymously.
//
// Used on routes that are public by default (e.g. /chat/completions) but
// have admin-only enrichment paths (memory injection, server-side session
// persistence) that key off userID when present.
func SoftJWTAuth(tm *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.Next()
			return
		}
		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := tm.Parse(tokenStr)
		if err != nil {
			c.Next()
			return
		}
		if claims.Type != auth.TypeAccess {
			c.Next()
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxUsername, claims.Username)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}
